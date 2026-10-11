// Type aliases over the types generated from api/openapi.yaml (npm run gen:api).
// Types only: no fetch client is introduced in Phase 1.
import type { components } from "./schema";

export type Event = components["schemas"]["Event"];
export type Alert = components["schemas"]["Alert"];
export type ErrorEnvelope = components["schemas"]["Error"];
export type EventList = components["schemas"]["EventList"];
export type AlertList = components["schemas"]["AlertList"];
