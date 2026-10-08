import { PageHeader } from "darkraise-ui/layout"
import { AccountCard } from "@/components/account-card"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"

export function SettingsPage() {
  const form = useSettingsForm()
  return (
    <>
      <PageHeader title="Settings" />
      {!form.config ? (
        <p className="text-sm text-muted-foreground">loading…</p>
      ) : (
        <>
          <SettingsSections form={form} />
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenSection />
            <RunnerSection />
            <AccountCard />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </>
  )
}
