import type { z } from "zod";
import type { SessionEventSchema, SessionInfo } from "./schemas";

function compare(a: SessionInfo, b: SessionInfo) {
  return (
    Date.parse(b.createdAt) - Date.parse(a.createdAt) ||
    a.id.localeCompare(b.id)
  );
}

/** Keep creation order without sorting the full list on every output update. */
export function applySessionEvent(
  sessions: SessionInfo[],
  event: z.infer<typeof SessionEventSchema>,
): SessionInfo[] {
  if (event.type === "snapshot") return [...event.sessions].sort(compare);
  const id = event.type === "removed" ? event.id : event.session.id;
  const index = sessions.findIndex((session) => session.id === id);
  if (event.type === "removed") {
    if (index === -1) return sessions;
    return sessions.filter((session) => session.id !== id);
  }
  const next = [...sessions];
  if (index !== -1) {
    if (sessions[index].createdAt === event.session.createdAt) {
      next[index] = event.session;
      return next;
    }
    next.splice(index, 1);
  }
  const position = next.findIndex(
    (session) => compare(event.session, session) < 0,
  );
  next.splice(position === -1 ? next.length : position, 0, event.session);
  return next;
}
