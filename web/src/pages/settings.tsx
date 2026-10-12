import { Link, useParams } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import type { ReactNode } from "react"
import { AccountSection } from "@/components/account-section"
import { ErrorLine } from "@/components/page/error-line"
import { SideGroup, SideLayout } from "@/components/page/side-layout"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"
import { SIDE_LINK } from "@/lib/side-link"
import { StoragePage } from "@/pages/storage"
import { ToolchainsPage } from "@/pages/toolchains"

const CONFIG = [
  { id: "", title: "General" },
  { id: "token", title: "GitHub token" },
  { id: "runner", title: "Runner software" },
  { id: "account", title: "Account" },
] as const

const HOST = [
  { id: "toolchains", title: "Toolchains" },
  { id: "storage", title: "Storage" },
] as const

function SectionLink({ id, title }: { id: string; title: string }) {
  return id === "" ? (
    <Link to="/settings" activeOptions={{ exact: true }} className={SIDE_LINK}>
      {title}
    </Link>
  ) : (
    <Link to="/settings/$section" params={{ section: id }} className={SIDE_LINK}>
      {title}
    </Link>
  )
}

// The save bar counts config fields only; the token, runner and account
// sections act at once with their own buttons.
function GeneralSettings() {
  const form = useSettingsForm()
  const c = form.config
  return (
    <>
      <PageHeader title="General" description={c ? `Serving ${c.owner} in ${c.mode} mode` : undefined} />
      {!c ? (
        form.configError ? <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine> : <Spinner label="Loading" />
      ) : (
        <>
          <div className="flex min-w-0 flex-col gap-6">
            <SettingsSections form={form} webAccess />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </>
  )
}

function Titled({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <PageHeader title={title} />
      {children}
    </>
  )
}

function SectionContent({ section }: { section: string }) {
  switch (section) {
    case "":
      return <GeneralSettings />
    case "token":
      return (
        <Titled title="GitHub token">
          <TokenSection />
        </Titled>
      )
    case "runner":
      return (
        <Titled title="Runner software">
          <RunnerSection />
        </Titled>
      )
    case "account":
      return (
        <Titled title="Account">
          <AccountSection />
        </Titled>
      )
    case "toolchains":
      return <ToolchainsPage />
    case "storage":
      return <StoragePage />
    default:
      return <p className="text-sm text-muted-foreground">Settings has no such section.</p>
  }
}

export function SettingsPage() {
  const { section = "" } = useParams({ strict: false })
  return (
    <SideLayout
      label="Settings"
      nav={
        <>
          {CONFIG.map((s) => (
            <SectionLink key={s.id} id={s.id} title={s.title} />
          ))}
          <SideGroup>Host</SideGroup>
          {HOST.map((s) => (
            <SectionLink key={s.id} id={s.id} title={s.title} />
          ))}
        </>
      }
    >
      <SectionContent section={section} />
    </SideLayout>
  )
}
