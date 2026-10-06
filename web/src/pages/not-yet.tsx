import { EmptyState } from "darkraise-ui/components/empty-state"
import { PageHeader } from "darkraise-ui/layout"

export function NotYet({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <EmptyState
        title="Not in the web UI yet"
        description="A later ghr release adds this page. Until then, run ghr on the runner host to use it in the terminal."
      />
    </>
  )
}
