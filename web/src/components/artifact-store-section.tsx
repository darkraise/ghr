import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { NumberField } from "@/components/number-field"
import { ConnectionCheck, KeyFields, WorkflowsCard } from "@/components/artifact-store-setup"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup, FieldInput } from "@/components/page/field"
import { SaveBar } from "@/components/save-bar"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { type StoreKey, useArtifactStoreForm } from "@/lib/artifact-store-form"
import { FIELD_WIDTH } from "@/lib/field-width"
import { numberFromText, textFromNumber } from "@/lib/number-text"

export function ArtifactStoreSection() {
  const form = useArtifactStoreForm()
  const { values, errors, offline } = form
  const changed = (...keys: StoreKey[]) => keys.some((k) => form.changed.includes(k))
  return (
    <>
      <PageHeader title="Self-hosted storage" description="Saved in config.yaml as artifact_store; the keys are kept in /etc/ghr/artifact-store-keys." />
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
            <FieldGroup id="artifact-connection" title="Connection">
              <Field
                label="Endpoint"
                htmlFor="artifact_endpoint"
                help="The S3 address ghr and its runners reach: https:// for any host, or http:// for a private, loopback or link-local IP address."
                error={errors.endpoint}
                changed={changed("endpoint")}
              >
                <FieldInput
                  id="artifact_endpoint"
                  className="w-full max-w-md font-mono"
                  placeholder="https://s3.example.com"
                  value={values.endpoint}
                  disabled={offline}
                  onChange={(e) => form.set("endpoint")(e.target.value)}
                />
              </Field>
              <Field label="Bucket" htmlFor="artifact_bucket" error={errors.bucket} changed={changed("bucket")}>
                <FieldInput
                  id="artifact_bucket"
                  className={`${FIELD_WIDTH} font-mono`}
                  value={values.bucket}
                  disabled={offline}
                  onChange={(e) => form.set("bucket")(e.target.value)}
                />
              </Field>
              <Field
                label="Region"
                htmlFor="artifact_region"
                help="RustFS accepts any region; keep us-east-1 unless your server says otherwise."
                changed={changed("region")}
              >
                <FieldInput
                  id="artifact_region"
                  className={`${FIELD_WIDTH} font-mono`}
                  placeholder="us-east-1"
                  value={values.region}
                  disabled={offline}
                  onChange={(e) => form.set("region")(e.target.value)}
                />
              </Field>
              <KeyFields offline={offline} />
              <ConnectionCheck offline={offline} />
            </FieldGroup>
            <FieldGroup id="artifact-policy" title="Policy">
              <Field
                label="Cleanup age"
                htmlFor="artifact_cleanup"
                help="Applies when a run sets no retention-days, such as 7d; empty is 7d, off keeps artifacts, and it is at least 24h. A repository's own age is set in its row on the Artifacts page."
                changed={changed("age")}
              >
                <FieldInput
                  id="artifact_cleanup"
                  className={`${FIELD_WIDTH} font-mono`}
                  placeholder="7d"
                  value={values.age}
                  disabled={offline}
                  onChange={(e) => form.set("age")(e.target.value)}
                />
              </Field>
              <Field
                label="Capacity"
                htmlFor="artifact_capacity"
                help="Optional. Shows use against the space you gave RustFS; empty shows totals only."
                changed={changed("capacity")}
              >
                <NumberField
                  id="artifact_capacity"
                  label="Capacity"
                  min={0}
                  precision={0}
                  suffix="GB"
                  className={FIELD_WIDTH}
                  value={numberFromText(values.capacity)}
                  disabled={offline}
                  onValueChange={(n) => form.set("capacity")(textFromNumber(n))}
                />
              </Field>
            </FieldGroup>
            <WorkflowsCard />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Self-hosted storage" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </>
  )
}
