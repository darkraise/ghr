// An address GitHub gave the daemon is only ever linked when it is https, so
// a javascript: or http: address can never become a link.
export function httpsUrl(url: string | undefined): string | undefined {
  return url?.startsWith("https://") ? url : undefined
}
