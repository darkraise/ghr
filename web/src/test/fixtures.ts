import configJson from "@/api/fixtures/config.json"
import containersJson from "@/api/fixtures/containers.json"
import eventsJson from "@/api/fixtures/events.json"
import historyJson from "@/api/fixtures/history.json"
import logJson from "@/api/fixtures/log.json"
import metricsJson from "@/api/fixtures/metrics.json"
import statusJson from "@/api/fixtures/status.json"
import stepsJson from "@/api/fixtures/steps.json"
import type { Config, Container, GhrEvent, HistoryEntry, LogChunk, Metrics, Status, Step } from "@/api/types"

const status: Status = statusJson
const events: GhrEvent[] = eventsJson
const history: HistoryEntry[] = historyJson
const log: LogChunk = logJson
const steps: Step[] = stepsJson
const containers: Container[] = containersJson
const metrics: Metrics = metricsJson
const config: Config = configJson

export const fixtures = { status, events, history, log, steps, containers, metrics, config }

export function authedRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "GET /auth/state": { setup_required: false, authenticated: true },
    "GET /api/status": fixtures.status,
    "GET /api/config": fixtures.config,
    "GET /api/metrics": fixtures.metrics,
    "GET /api/events": fixtures.events,
    ...over,
  }
}
