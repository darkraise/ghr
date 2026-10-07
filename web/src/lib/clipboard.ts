import { toast } from "darkraise-ui/components/sonner"
import { errorText } from "@/query"

export async function copyText(text: string): Promise<void> {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(text)
    return
  }
  // navigator.clipboard exists only on HTTPS or localhost; the web listener
  // is plain HTTP on the LAN, so fall back to a hidden textarea.
  const area = document.createElement("textarea")
  area.value = text
  area.setAttribute("readonly", "")
  area.style.position = "fixed"
  area.style.opacity = "0"
  document.body.appendChild(area)
  area.select()
  try {
    if (!document.execCommand("copy")) throw new Error("the browser refused to copy")
  } finally {
    area.remove()
  }
}

export async function copyWithToast(text: string, label: string): Promise<void> {
  try {
    await copyText(text)
    toast.success(`copied ${label}`)
  } catch (err) {
    toast.error(errorText(err))
  }
}
