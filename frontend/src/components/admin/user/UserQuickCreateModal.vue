<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.quickCreate.title')"
    width="normal"
    :show-close-button="!submitting"
    :close-on-escape="!submitting"
    @close="close"
  >
    <form v-if="!result" id="quick-create-user-form" class="space-y-5" @submit.prevent="submit">
      <div>
        <label class="input-label">{{ t('admin.users.quickCreate.group') }}</label>
        <Select
          v-model="groupId"
          :disabled="submitting || loadingGroups"
          :options="groupOptions"
          :placeholder="t('admin.users.quickCreate.selectGroup')"
        >
          <template #selected="{ option }">
            <GroupBadge
              v-if="option"
              :name="String(option.label)"
              :platform="groupMeta(option.value)?.platform"
              :subscription-type="groupMeta(option.value)?.subscription_type"
              :rate-multiplier="groupMeta(option.value)?.rate_multiplier"
            />
            <span v-else class="text-gray-400">{{ t('admin.users.quickCreate.selectGroup') }}</span>
          </template>
          <template #option="{ option, selected }">
            <GroupOptionItem
              :name="String(option.label)"
              :platform="groupMeta(option.value)?.platform || 'openai'"
              :subscription-type="groupMeta(option.value)?.subscription_type || 'subscription'"
              :rate-multiplier="groupMeta(option.value)?.rate_multiplier || 1"
              :description="groupMeta(option.value)?.description"
              :selected="selected"
            />
          </template>
        </Select>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.quickCreate.validityDays') }}</label>
        <input v-model.number="validityDays" type="number" min="1" max="36500" step="1" :disabled="submitting" class="input" required />
        <p class="input-hint">{{ t('admin.users.quickCreate.validityHint') }}</p>
      </div>
    </form>

    <div v-else class="space-y-3" data-test="quick-create-result">
      <div v-for="row in rows" :key="row.label" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div class="mb-1 flex items-center justify-between gap-3">
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ row.label }}</span>
          <button type="button" class="btn btn-secondary btn-sm" @click="copy(row.value)">{{ t('admin.users.quickCreate.copy') }}</button>
        </div>
        <p class="break-all text-sm text-gray-900 dark:text-white">{{ row.value }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">{{ t('common.close') }}</button>
        <button v-if="result" type="button" class="btn btn-primary" @click="copy(allText)">{{ t('admin.users.quickCreate.copyAll') }}</button>
        <button v-else type="submit" form="quick-create-user-form" class="btn btn-primary" :disabled="submitting || !groupId">
          {{ submitting ? t('admin.users.quickCreate.creating') : t('admin.users.quickCreate.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { QuickCreateUserResult } from '@/api/admin/users'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; success: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const groups = ref<AdminGroup[]>([])
const loadingGroups = ref(false)
const submitting = ref(false)
const groupId = ref<number | null>(null)
const validityDays = ref(30)
const result = ref<QuickCreateUserResult | null>(null)

const groupOptions = computed(() =>
  groups.value
    .filter((group) => group.subscription_type === 'subscription' && group.status === 'active')
    .map((group) => ({ value: group.id, label: group.name }))
)

const groupMeta = (value: string | number | boolean | null | undefined) =>
  groups.value.find((group) => group.id === Number(value))

const rows = computed(() => {
  if (!result.value) return []
  const website = result.value.website_url.trim() || window.location.origin
  const configuredRequest = result.value.request_url.trim()
  const requestURL = !configuredRequest || configuredRequest === '/v1'
    ? `${website.replace(/\/$/, '')}/v1`
    : configuredRequest
  return [
    { label: t('admin.users.quickCreate.website'), value: website || '-' },
    { label: t('admin.users.quickCreate.email'), value: result.value.email },
    { label: t('admin.users.quickCreate.password'), value: result.value.password },
    { label: t('admin.users.quickCreate.requestUrl'), value: requestURL || '-' },
    { label: t('admin.users.quickCreate.key'), value: result.value.api_key },
    { label: t('admin.users.quickCreate.plan'), value: `${result.value.group_name}（${t('admin.users.quickCreate.validityValue', { days: result.value.validity_days })}）` },
    { label: t('admin.users.quickCreate.models'), value: result.value.models_label || t('admin.users.quickCreate.modelsValue') },
  ]
})

const allText = computed(() => rows.value.map((row) => `${row.label}：${row.value}`).join('\n'))

const reset = () => {
  groupId.value = null
  validityDays.value = 30
  result.value = null
}

const loadGroups = async () => {
  loadingGroups.value = true
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.quickCreate.failed'))
  } finally {
    loadingGroups.value = false
  }
}

watch(() => props.show, (open) => {
  if (!open) return
  reset()
  void loadGroups()
})

const close = () => {
  if (submitting.value) return
  emit('close')
}

const copy = (value: string) => {
  copyToClipboard(value, t('admin.users.quickCreate.copied'))
}

const submit = async () => {
  if (!groupId.value || validityDays.value < 1 || validityDays.value > 36500) return
  submitting.value = true
  try {
    result.value = await adminAPI.users.quickCreate(groupId.value, validityDays.value)
    emit('success')
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.message || t('admin.users.quickCreate.failed'))
  } finally {
    submitting.value = false
  }
}
</script>
