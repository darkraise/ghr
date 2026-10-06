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
