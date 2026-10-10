import { useState } from "react"
import type { GraphNode, RunGraph, RunJob } from "@/api/types"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { jobMs, statusText } from "@/lib/run-details"

const TILE_W = 220
const GAP_X = 70
const GAP_Y = 16
const HEAD_H = 40
const ROW_H = 28
const GROUP_SHOWN = 5

interface Placed {
  node: GraphNode
  x: number
  y: number
  h: number
}

function tileHeight(node: GraphNode, open: boolean): number {
  if (!node.group) return HEAD_H
  const rows = open ? node.job_ids.length : Math.min(node.job_ids.length, GROUP_SHOWN)
  const toggle = node.job_ids.length > GROUP_SHOWN ? ROW_H : 0
  return HEAD_H + rows * ROW_H + toggle + 8
}

// Nodes arrive sorted by column, then by their order in the workflow file.
function layout(nodes: GraphNode[], open: Set<string>): { placed: Placed[]; width: number; height: number } {
  const colY: number[] = []
  const placed = nodes.map((node) => {
    const y = colY[node.column] ?? 0
    const h = tileHeight(node, open.has(node.key))
    colY[node.column] = y + h + GAP_Y
    return { node, x: node.column * (TILE_W + GAP_X), y, h }
  })
  const columns = Math.max(1, ...nodes.map((n) => n.column + 1))
  return { placed, width: columns * TILE_W + (columns - 1) * GAP_X, height: Math.max(HEAD_H, ...colY.map((y) => y - GAP_Y)) }
}

// Lines leave a node's right edge and enter each dependent's left edge, both
// at the middle of the tile's first row.
function edges(placed: Placed[]): string[] {
  const at = new Map(placed.map((p) => [p.node.key, p]))
  return placed.flatMap((to) =>
    to.node.needs.flatMap((key) => {
      const from = at.get(key)
      if (!from) return []
      const x1 = from.x + TILE_W
      const y1 = from.y + HEAD_H / 2
      const x2 = to.x
      const y2 = to.y + HEAD_H / 2
      const bend = GAP_X / 2
      return [`M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`]
    }),
  )
}

function JobLine({ job, now }: { job: RunJob; now: number }) {
  const ms = jobMs(job, now)
  return (
    <>
      <RunStatusIcon status={job.status} conclusion={job.conclusion} />
      <span className="min-w-0 flex-1 truncate">{job.name}</span>
      {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
    </>
  )
}

export function JobGraph({
  graph,
  jobs,
  path,
  event,
  now,
  jobLink,
}: {
  graph: RunGraph
  jobs: RunJob[]
  path: string
  event: string
  now: number
  jobLink: JobLinkFn
}) {
  const [open, setOpen] = useState<Set<string>>(new Set())
  const byId = new Map(jobs.map((j) => [j.id, j]))
  const names = new Map(graph.nodes.map((n) => [n.key, n.name]))
  const { placed, width, height } = layout(graph.nodes, open)

  function label(job: RunJob, node: GraphNode): string {
    const needs = node.needs.map((k) => names.get(k) ?? k)
    return `${job.name}, ${statusText(job.status, job.conclusion)}${needs.length > 0 ? `, needs ${needs.join(", ")}` : ""}`
  }

  function toggle(key: string) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <Section
      title={path.split("/").pop() ?? path}
      aside={
        event ? (
          <span className="text-sm text-muted-foreground">
            on: <span className="font-mono">{event}</span>
          </span>
        ) : undefined
      }
    >
      <div role="group" aria-label="Job graph" tabIndex={0} className="overflow-x-auto rounded-md bg-background p-3">
        <div className="relative" style={{ width, height }}>
          <svg aria-hidden="true" width={width} height={height} className="absolute inset-0 text-muted-foreground">
            {edges(placed).map((d) => (
              <path key={d} d={d} fill="none" stroke="currentColor" strokeWidth={1} />
            ))}
          </svg>
          {placed.map(({ node, x, y, h }) => {
            const style = { left: x, top: y, width: TILE_W, height: h }
            const first = node.job_ids[0]
            const single = first !== undefined ? byId.get(first) : undefined
            if (!node.group && single) {
              return (
                <div key={node.key} className="absolute" style={style}>
                  {jobLink(single.id, <JobLine job={single} now={now} />, {
                    className: "flex h-full items-center gap-2 rounded-md border border-border bg-card hover:border-primary focus-visible:border-primary px-3 text-sm",
                    label: label(single, node),
                  })}
                </div>
              )
            }
            const isOpen = open.has(node.key)
            const members = node.job_ids.flatMap((id) => {
              const j = byId.get(id)
              return j ? [j] : []
            })
            const shown = isOpen ? members : members.slice(0, GROUP_SHOWN)
            const hidden = members.length - GROUP_SHOWN
            return (
              <div key={node.key} className="absolute flex flex-col rounded-md border border-border bg-card hover:border-primary focus-within:border-primary px-3 py-1" style={style}>
                <p className="flex h-8 items-center gap-2 text-sm font-semibold">
                  <span className="min-w-0 flex-1 truncate">{node.name}</span>
                  <span className="font-mono text-xs text-muted-foreground">{members.length}</span>
                </p>
                <ul className="flex flex-col">
                  {shown.map((j) => (
                    <li key={j.id} className="h-7">
                      {jobLink(j.id, <JobLine job={j} now={now} />, {
                        className: "flex h-full items-center gap-2 rounded-[4px] px-1 text-sm hover:bg-muted",
                        label: label(j, node),
                      })}
                    </li>
                  ))}
                </ul>
                {hidden > 0 && (
                  <button
                    type="button"
                    aria-expanded={isOpen}
                    onClick={() => toggle(node.key)}
                    className="h-7 px-1 text-left text-sm text-primary hover:underline"
                  >
                    {isOpen ? "Show fewer" : `${hidden} more`}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </Section>
  )
}
