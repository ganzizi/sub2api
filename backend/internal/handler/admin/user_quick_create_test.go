package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type fakeQuickAdmin struct {
	group     *service.Group
	getErr    error
	createErr error
	deleteErr error
	created   []*service.User
	deleted   []int64
	lastInput *service.CreateUserInput
}

func (f *fakeQuickAdmin) CreateUser(_ context.Context, input *service.CreateUserInput) (*service.User, error) {
	f.lastInput = input
	if f.createErr != nil {
		return nil, f.createErr
	}
	user := &service.User{ID: int64(len(f.created) + 1), Email: input.Email}
	f.created = append(f.created, user)
	return user, nil
}

func (f *fakeQuickAdmin) GetGroup(context.Context, int64) (*service.Group, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.group, nil
}

func (f *fakeQuickAdmin) DeleteUser(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

type fakeQuickSubs struct {
	err    error
	inputs []*service.AssignSubscriptionInput
}

func (f *fakeQuickSubs) AssignOrExtendSubscription(_ context.Context, input *service.AssignSubscriptionInput) (*service.UserSubscription, bool, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return nil, false, f.err
	}
	return &service.UserSubscription{UserID: input.UserID, GroupID: input.GroupID}, false, nil
}

type fakeQuickKeys struct {
	err error
	key *service.APIKey
}

func (f *fakeQuickKeys) Create(_ context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.key != nil {
		return f.key, nil
	}
	return &service.APIKey{UserID: userID, Key: "sk-test", GroupID: req.GroupID, Name: req.Name}, nil
}

func TestQuickRequestURL(t *testing.T) {
	t.Parallel()
	if got := quickRequestURL(" https://api.example.com/ ", ""); got != "https://api.example.com/v1" {
		t.Fatalf("api base = %s", got)
	}
	if got := quickRequestURL("https://api.example.com/v1/", ""); got != "https://api.example.com/v1" {
		t.Fatalf("existing v1 = %s", got)
	}
	if got := quickRequestURL("", "https://panel.example.com"); got != "https://panel.example.com/v1" {
		t.Fatalf("frontend fallback = %s", got)
	}
	if got := quickRequestURL("  ", " "); got != "/v1" {
		t.Fatalf("empty = %s", got)
	}
}

func TestRunQuickCreateSuccess(t *testing.T) {
	t.Parallel()
	adminFake := &fakeQuickAdmin{group: &service.Group{ID: 11, Name: "组合-月套餐", Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}}
	subs := &fakeQuickSubs{}
	keys := &fakeQuickKeys{}
	result, err := runQuickCreate(context.Background(), adminFake, subs, keys, quickCreateEnv{
		FrontendURL: "https://api.go-oai.com",
		APIBaseURL:  "https://api.go-oai.com",
		Concurrency: 3,
		RPMLimit:    0,
		ActorAdmin:  9,
	}, 11, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.GroupName != "组合-月套餐" || result.ValidityDays != 30 || result.APIKey != "sk-test" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.HasSuffix(result.Email, "@local.com") || len(result.Password) != quickCreatePasswordLen {
		t.Fatalf("credentials email=%s password=%s", result.Email, result.Password)
	}
	if result.WebsiteURL != "https://api.go-oai.com" || result.RequestURL != "https://api.go-oai.com/v1" {
		t.Fatalf("urls website=%s request=%s", result.WebsiteURL, result.RequestURL)
	}
	if result.ModelsLabel != quickCreateModelsLabel {
		t.Fatalf("models = %s", result.ModelsLabel)
	}
	if adminFake.lastInput == nil || adminFake.lastInput.Concurrency != 3 || adminFake.lastInput.Role != service.RoleUser {
		t.Fatalf("create input = %#v", adminFake.lastInput)
	}
	if len(subs.inputs) != 1 || subs.inputs[0].ValidityDays != 30 || subs.inputs[0].GroupID != 11 {
		t.Fatalf("subscription = %#v", subs.inputs)
	}
	if len(adminFake.deleted) != 0 {
		t.Fatalf("unexpected rollback %#v", adminFake.deleted)
	}
}

func TestRunQuickCreateRejectsNonSubscriptionGroup(t *testing.T) {
	t.Parallel()
	adminFake := &fakeQuickAdmin{group: &service.Group{ID: 1, Name: "default", Status: service.StatusActive, SubscriptionType: "standard"}}
	_, err := runQuickCreate(context.Background(), adminFake, &fakeQuickSubs{}, &fakeQuickKeys{}, quickCreateEnv{}, 1, 7)
	if err == nil || !strings.Contains(err.Error(), "invalid subscription group") {
		t.Fatalf("err = %v", err)
	}
	if len(adminFake.created) != 0 {
		t.Fatal("user was created")
	}
}

func TestRunQuickCreateRollsBackWhenKeyFails(t *testing.T) {
	t.Parallel()
	adminFake := &fakeQuickAdmin{group: &service.Group{ID: 9, Name: "组合-日套餐", Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}}
	_, err := runQuickCreate(context.Background(), adminFake, &fakeQuickSubs{}, &fakeQuickKeys{err: errors.New("key failed")}, quickCreateEnv{}, 9, 1)
	if err == nil || !strings.Contains(err.Error(), "key failed") {
		t.Fatalf("err = %v", err)
	}
	if len(adminFake.deleted) != 1 || adminFake.deleted[0] != adminFake.created[0].ID {
		t.Fatalf("deleted=%v created=%v", adminFake.deleted, adminFake.created)
	}
}
