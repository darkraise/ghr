import { Link } from "@tanstack/react-router"
import { Input } from "darkraise-ui/components/input"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { CleanupPreview } from "@/components/cleanup-preview"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { SaveBar } from "@/components/save-bar"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { FIELD_WIDTH } from "@/lib/field-width"
import { CLEANUP_WARNING } from "@/lib/quota"
import { type PolicyKey, useStoragePolicyForm } from "@/lib/storage-policy-form"

export function GitHubStorageSection() {
  const form = useStoragePolicyForm()
  const { values, errors, offline, pending } = form
  const changed = (...keys: PolicyKey[]) => keys.some((k) => form.changed.includes(k))
  const confirm: Confirm | null = pending && {
    title: `Clean artifacts after ${pending.age}?`,
    body: CLEANUP_WARNING,
    details: <CleanupPreview age={pending.age} previews={pending.previews} />,
    action: "Save",
    destructive: pending.previews.some((p) => p.count > 0),
    run: form.confirmPending,
  }
  return (
    <>
      <PageHeader title="GitHub storage" description="Saved in config.yaml as github_storage." />
      {!form.config ? (
        form.configError ? (
          <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine>
        ) : (
          <Spinner label="Loading" />
        )
      ) : (
        <>
          <div className="flex min-w-0 flex-col gap-4">
            {form.rejection && <ErrorLine>{form.rejection}</ErrorLine>}
            <FieldGroup id="storage-policy" title="Policy">
              <Field label="Included storage" htmlFor="included_mb" help="The plan's allowance for artifacts and Packages, in MB." error={errors.included} changed={changed("included")}>
                <Input id="included_mb" type="number" min={1} suffix="MB" groupClassName={FIELD_WIDTH} className="min-w-0" value={values.included} disabled={offline} onChange={(e) => form.set("included")(e.target.value)} />
              </Field>
              <Field label="Warning levels" help="Warn, then warn louder, when the estimate reaches these shares of the allowance." error={errors.critical} changed={changed("warn", "critical")}>
                <Input aria-label="Warning at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.warn} disabled={offline} onChange={(e) => form.set("warn")(e.target.value)} />
                <Input aria-label="Critical at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.critical} disabled={offline} onChange={(e) => form.set("critical")(e.target.value)} />
              </Field>
              <Field label="Cleanup age" htmlFor="cleanup_max_age" help={`Delete artifacts older than this, such as 2d; empty is off, and at least 24h. ${CLEANUP_WARNING}`} changed={changed("age")}>
                <Input id="cleanup_max_age" className={`${FIELD_WIDTH} font-mono`} placeholder="off" value={values.age} disabled={offline} onChange={(e) => form.set("age")(e.target.value)} />
              </Field>
            </FieldGroup>
            <p className="text-sm text-muted-foreground">
              A repository's own cleanup age and retention are set in its row on the{" "}
              <Link to="/artifacts" className="text-primary hover:underline">
                Artifacts page
              </Link>
              .
            </p>
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="GitHub storage" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
          <ConfirmDialog confirm={confirm} onClose={form.cancelPending} />
        </>
      )}
    </>
  )
}
