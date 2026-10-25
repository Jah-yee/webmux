import {
  HomeWorktreesSchema,
  type HomeWorktrees,
  type Repository,
  type SessionInfo,
  type Worktree,
} from "./schemas";

/** Unreadable stored data degrades to "never curated" for every repository. */
export function parseHomeWorktrees(raw: string): HomeWorktrees {
  if (!raw) return {};
  try {
    const result = HomeWorktreesSchema.safeParse(JSON.parse(raw));
    return result.success ? result.data : {};
  } catch {
    return {};
  }
}

/** A repository the user never curated shows only its main worktree. */
function pinnedIds(home: HomeWorktrees, repo: Repository): ReadonlySet<string> {
  return new Set(
    home[repo.id] ??
      repo.worktrees.filter((tree) => tree.kind === "main").map((t) => t.id),
  );
}

export function worktreeTitle(tree: Worktree) {
  return (
    tree.branch ||
    (tree.detached
      ? `detached · ${tree.head?.slice(0, 12)}`
      : tree.kind === "bare"
        ? "Bare 仓库"
        : "尚无提交")
  );
}

export interface HomeLayout {
  /** Pinned worktrees plus any worktree that still has sessions. */
  shown: { tree: Worktree; pinned: boolean }[];
  /** Every worktree not pinned, including ones shown only for their sessions. */
  unpinned: Worktree[];
}

// Sessions are never hidden: a worktree with sessions stays on the home page
// whether or not it is pinned, so curation only removes idle clutter.
export function homeLayout(
  home: HomeWorktrees,
  repo: Repository,
  sessions: readonly SessionInfo[],
): HomeLayout {
  const pinned = pinnedIds(home, repo);
  const active = new Set(
    sessions
      .filter((s) => s.workspace?.repositoryId === repo.id)
      .map((s) => s.workspace?.worktreeId),
  );
  const trees = [...repo.worktrees].sort((a, b) =>
    a.kind === "main"
      ? -1
      : b.kind === "main"
        ? 1
        : a.path < b.path
          ? -1
          : a.path > b.path
            ? 1
            : 0,
  );
  return {
    shown: trees
      .filter((tree) => pinned.has(tree.id) || active.has(tree.id))
      .map((tree) => ({ tree, pinned: pinned.has(tree.id) })),
    unpinned: trees.filter((tree) => !pinned.has(tree.id)),
  };
}

export function setPinned(
  home: HomeWorktrees,
  repo: Repository,
  ids: readonly string[],
  pinned: boolean,
): HomeWorktrees {
  const next = new Set(pinnedIds(home, repo));
  for (const id of ids) {
    if (pinned) next.add(id);
    else next.delete(id);
  }
  return { ...home, [repo.id]: [...next] };
}

/**
 * Drops entries of unregistered repositories and pins of worktrees that a
 * healthy snapshot no longer lists. Unhealthy snapshots keep their pins so a
 * transient Git failure cannot erase the user's choice. Returns `home` itself
 * when nothing changed.
 */
export function pruneHomeWorktrees(
  home: HomeWorktrees,
  repositories: readonly Repository[],
): HomeWorktrees {
  const next: HomeWorktrees = {};
  let changed = false;
  for (const [id, ids] of Object.entries(home)) {
    const repo = repositories.find((r) => r.id === id);
    if (!repo) {
      changed = true;
      continue;
    }
    if (repo.availability !== "available" || repo.error) {
      next[id] = ids;
      continue;
    }
    const kept = ids.filter((tree) =>
      repo.worktrees.some((t) => t.id === tree),
    );
    if (kept.length !== ids.length) changed = true;
    next[id] = kept;
  }
  return changed ? next : home;
}
