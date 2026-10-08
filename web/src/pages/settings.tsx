import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { AccountSection } from "@/components/account-section"
import { ErrorLine } from "@/components/page/error-line"
import { SectionNav } from "@/components/page/section-nav"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"

const SECTIONS = [
  { id: "general", title: "General" },
  { id: "timing", title: "Timing" },
  { id: "disk", title: "Disk and retention" },
  { id: "runner-defaults", title: "Runner defaults" },
  { id: "token", title: "GitHub token" },
  { id: "runner", title: "Runner and config" },
  { id: "account", title: "Account" },
] as const

// The save bar counts config fields only; the token, runner and account
// sections act at once with their own buttons.
export function SettingsPage() {
  const form = useSettingsForm()
  const c = form.config
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Settings" description={c ? `Serving ${c.owner} in ${c.mode} mode` : undefined} />
      {!c ? (
        form.configError ? <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine> : <Spinner label="Loading" />
      ) : (
        <>
          <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_12rem]">
            <div className="flex min-w-0 flex-col gap-6">
              <SettingsSections form={form} />
              <TokenSection />
              <RunnerSection />
              <AccountSection />
            </div>
            <SectionNav items={SECTIONS} />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </div>
  )
}
