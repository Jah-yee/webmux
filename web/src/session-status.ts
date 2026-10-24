import type { SessionInfo } from "./schemas";

export type SessionState = SessionInfo["status"];

export interface SessionCounts {
  // "Running" in list summaries covers both busy and quiet live sessions.
  active: number;
  waiting: number;
  exited: number;
}

export function isActive(status: SessionState) {
  return status === "running" || status === "idle";
}

export type StatusFilter = "all" | "active" | "waiting" | "exited";

export function matchesStatusFilter(
  status: SessionState,
  filter: StatusFilter,
) {
  if (filter === "all") return true;
  return filter === "active" ? isActive(status) : status === filter;
}

export function countSessions(
  sessions: readonly Pick<SessionInfo, "status">[],
): SessionCounts {
  const counts: SessionCounts = { active: 0, waiting: 0, exited: 0 };
  for (const { status } of sessions) {
    if (isActive(status)) counts.active++;
    else if (status === "waiting") counts.waiting++;
    else if (status === "exited") counts.exited++;
  }
  return counts;
}

export function statusSummary(
  sessions: readonly Pick<SessionInfo, "status">[],
) {
  const { active, waiting, exited } = countSessions(sessions);
  return `运行 ${active} · 等待 ${waiting} · 退出 ${exited}`;
}
