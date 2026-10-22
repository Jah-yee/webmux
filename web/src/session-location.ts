import type { ReportedCwd, SessionInfo } from "./schemas";

export interface LocationHints {
  launch: string | null;
  reported: ReportedCwd | null;
}

// Shell reports are navigation hints. Grouping and ownership stay on the
// immutable launch directory and workspace association.
export function locationHints(
  session: Pick<SessionInfo, "cwd" | "reportedCwd">,
  groupPath?: string,
): LocationHints {
  const reported = session.reportedCwd;
  return {
    launch:
      groupPath !== undefined && session.cwd === groupPath ? null : session.cwd,
    reported:
      reported && (reported.remote || reported.path !== session.cwd)
        ? reported
        : null,
  };
}

export function matchesSessionQuery(
  session: SessionInfo,
  search: string,
  repositoryName?: string,
  branch?: string | null,
) {
  return [
    session.title,
    session.cwd,
    session.reportedCwd?.path,
    session.reportedCwd?.host,
    repositoryName,
    branch,
  ].some((value) => value?.toLocaleLowerCase().includes(search));
}
