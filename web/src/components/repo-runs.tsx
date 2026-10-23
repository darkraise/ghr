import { Link } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { useActions } from "@/api/hooks"
import { ActionsRunTable } from "@/components/actions-run-table"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { repoErrorText, splitRuns } from "@/lib/actions"
import { useMediaQuery } from "@/lib/use-media-query"
import { errorText } from "@/query"

const RECENT = 10

export function RepoRuns({ name, now }: { name: string; now: number }) {
  const actions = useActions()
  const wide = useMediaQuery("(min-width: 768px)")
  const data = actions.data
  const failed = data?.repos.find((r) => r.error && r.repo.toLowerCase() === name.toLowerCase())
  const { active, recent } = data ? splitRuns(data.runs, name, undefined) : { active: [], recent: [] }

  const retry = actions.isError ? <ErrorLine onRetry={() => void actions.refetch()}>{errorText(actions.error)}</ErrorLine> : null
  const all = (
    <Link to="/actions" search={{ repo: name }} className="ml-auto text-sm text-primary hover:underline">
      All runs in Actions
    </Link>
  )
  return (
    <Section title="Workflow runs" aside={all} flush={Boolean(data)}>
      {data ? (
        <>
          {(retry || failed) && (
            <div className="flex flex-col gap-1 border-b border-border px-4 py-2.5">
              {retry}
              {failed && <ErrorLine>{repoErrorText(failed)}</ErrorLine>}
            </div>
          )}
          <ActionsRunTable active={active} recent={recent.slice(0, RECENT)} now={now} wide={wide} showActive />
        </>
      ) : (
        (retry ?? <Spinner label="Loading" />)
      )}
    </Section>
  )
}
