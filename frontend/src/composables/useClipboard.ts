import { ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { i18n } from '@/i18n'

const { t } = i18n.global

/**
 * 检测是否支持 Clipboard API（需要安全上下文：HTTPS/localhost）
 */
function isClipboardSupported(): boolean {
  return !!(navigator.clipboard && window.isSecureContext)
}

/**
 * 降级方案：使用 textarea + execCommand
 * 使用 textarea 而非 input，以正确处理多行文本
 */
function fallbackCopy(text: string): boolean {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', 'true')
  // 1px 高会让部分浏览器的 execCommand 丢掉最后一行，模型文案就复制不上去。
  textarea.style.cssText = 'position:fixed;top:0;left:-9999px;width:800px;height:400px;opacity:0;pointer-events:none'
  document.body.appendChild(textarea)
  textarea.focus({ preventScroll: true })
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    document.body.removeChild(textarea)
  }
}

export function useClipboard() {
  const appStore = useAppStore()
  const copied = ref(false)

  const copyToClipboard = async (
    text: string,
    successMessage?: string
  ): Promise<boolean> => {
    if (!text) return false

    let success = false

    if (isClipboardSupported()) {
      try {
        await navigator.clipboard.writeText(text)
        success = true
      } catch {
        success = fallbackCopy(text)
      }
    } else {
      success = fallbackCopy(text)
    }

    if (success) {
      copied.value = true
      appStore.showSuccess(successMessage || t('common.copiedToClipboard'))
      setTimeout(() => {
        copied.value = false
      }, 2000)
    } else {
      appStore.showError(t('common.copyFailed'))
    }

    return success
  }

  return { copied, copyToClipboard }
}
