package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	quickCreateMaxValidityDays = 36500
	quickCreatePasswordLen     = 12
	quickCreateModelsLabel     = "deepseek-v4.1-flash（1M/多模态）、glm-5.3-flash（1M/多模态）、deep（500KB/多模态/🔥复杂任务推荐）"
)

const quickCreatePasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// quickSubscriptionAssigner 复用订阅分配，不改原分配实现。
type quickSubscriptionAssigner interface {
	AssignOrExtendSubscription(ctx context.Context, input *service.AssignSubscriptionInput) (*service.UserSubscription, bool, error)
}

// quickAPIKeyCreator 复用用户 API 密钥创建，不改原创建实现。
type quickAPIKeyCreator interface {
	Create(ctx context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error)
}

// QuickCreateUserRequest 快捷创建只需要订阅分组和有效期。
type QuickCreateUserRequest struct {
	GroupID      int64 `json:"group_id" binding:"required,min=1"`
	ValidityDays int   `json:"validity_days" binding:"required,min=1,max=36500"`
}

// QuickCreateUserResponse 返回可复制的开通信息。密码和密钥只在这次响应里出现。
type QuickCreateUserResponse struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	APIKey       string `json:"api_key"`
	WebsiteURL   string `json:"website_url"`
	RequestURL   string `json:"request_url"`
	GroupID      int64  `json:"group_id"`
	GroupName    string `json:"group_name"`
	ValidityDays int    `json:"validity_days"`
	ModelsLabel  string `json:"models_label"`
}

type quickCreateEnv struct {
	FrontendURL string
	APIBaseURL  string
	Concurrency int
	RPMLimit    int
	ActorAdmin  int64
}

// QuickCreate 生成随机用户、分配订阅并创建 API 密钥。
// POST /api/v1/admin/users/quick-create
func (h *UserHandler) QuickCreate(c *gin.Context) {
	if h == nil || h.adminService == nil || h.quickSubs == nil || h.quickKeys == nil {
		response.InternalError(c, "quick create is not configured")
		return
	}
	var req QuickCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := runQuickCreate(c.Request.Context(), h.adminService, h.quickSubs, h.quickKeys, h.quickCreateEnv(c), req.GroupID, req.ValidityDays)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid ") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserHandler) quickCreateEnv(c *gin.Context) quickCreateEnv {
	env := quickCreateEnv{Concurrency: 1, ActorAdmin: getAdminIDFromContext(c)}
	if h.settingService == nil {
		return env
	}
	ctx := c.Request.Context()
	env.FrontendURL = h.settingService.GetFrontendURL(ctx)
	if publicSettings, err := h.settingService.GetPublicSettings(ctx); err == nil && publicSettings != nil {
		env.APIBaseURL = publicSettings.APIBaseURL
	}
	if concurrency := h.settingService.GetDefaultConcurrency(ctx); concurrency > 0 {
		env.Concurrency = concurrency
	}
	env.RPMLimit = h.settingService.GetDefaultUserRPMLimit(ctx)
	return env
}

type quickCreateAdmin interface {
	CreateUser(ctx context.Context, input *service.CreateUserInput) (*service.User, error)
	GetGroup(ctx context.Context, id int64) (*service.Group, error)
	DeleteUser(ctx context.Context, id int64) error
}

func runQuickCreate(ctx context.Context, admin quickCreateAdmin, subs quickSubscriptionAssigner, keys quickAPIKeyCreator, env quickCreateEnv, groupID int64, validityDays int) (*QuickCreateUserResponse, error) {
	if groupID <= 0 {
		return nil, fmt.Errorf("invalid group_id")
	}
	if validityDays < 1 || validityDays > quickCreateMaxValidityDays {
		return nil, fmt.Errorf("invalid validity_days")
	}
	group, err := admin.GetGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || !group.IsActive() || !group.IsSubscriptionType() {
		return nil, fmt.Errorf("invalid subscription group")
	}

	var created *service.User
	var password string
	for attempt := 0; attempt < 3; attempt++ {
		email, generatedPassword, genErr := newQuickCreateCredentials()
		if genErr != nil {
			return nil, genErr
		}
		user, createErr := admin.CreateUser(ctx, &service.CreateUserInput{
			Email:        email,
			Password:     generatedPassword,
			Username:     strings.SplitN(email, "@", 2)[0],
			Notes:        "quick-create",
			Role:         service.RoleUser,
			Concurrency:  env.Concurrency,
			RPMLimit:     env.RPMLimit,
			ActorAdminID: env.ActorAdmin,
		})
		if createErr != nil {
			err = createErr
			continue
		}
		created = user
		password = generatedPassword
		err = nil
		break
	}
	if created == nil {
		if err == nil {
			err = fmt.Errorf("create user failed")
		}
		return nil, err
	}

	rollback := func(cause error) error {
		if deleteErr := admin.DeleteUser(ctx, created.ID); deleteErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", cause, deleteErr)
		}
		return cause
	}

	if _, _, err = subs.AssignOrExtendSubscription(ctx, &service.AssignSubscriptionInput{
		UserID:       created.ID,
		GroupID:      group.ID,
		ValidityDays: validityDays,
		AssignedBy:   env.ActorAdmin,
		Notes:        "quick-create",
	}); err != nil {
		return nil, rollback(err)
	}

	groupIDCopy := group.ID
	apiKey, err := keys.Create(ctx, created.ID, service.CreateAPIKeyRequest{
		Name:    "key",
		GroupID: &groupIDCopy,
	})
	if err != nil {
		return nil, rollback(err)
	}
	if apiKey == nil || apiKey.Key == "" {
		return nil, rollback(fmt.Errorf("api key was not returned"))
	}

	return &QuickCreateUserResponse{
		Email:        created.Email,
		Password:     password,
		APIKey:       apiKey.Key,
		WebsiteURL:   strings.TrimSpace(env.FrontendURL),
		RequestURL:   quickRequestURL(env.APIBaseURL, env.FrontendURL),
		GroupID:      group.ID,
		GroupName:    group.Name,
		ValidityDays: validityDays,
		ModelsLabel:  quickCreateModelsLabel,
	}, nil
}

func newQuickCreateCredentials() (string, string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", fmt.Errorf("generate user: %w", err)
	}
	email := "q" + hex.EncodeToString(random[:]) + "@local.com"
	password, err := randomPassword(quickCreatePasswordLen)
	if err != nil {
		return "", "", err
	}
	return email, password, nil
}

func randomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("generate password: invalid length")
	}
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	alphabet := quickCreatePasswordAlphabet
	buf := make([]byte, length)
	for i, value := range raw {
		buf[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(buf), nil
}

func quickRequestURL(apiBase, frontend string) string {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		base = strings.TrimSpace(frontend)
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return "/v1"
	}
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base
	}
	return base + "/v1"
}
