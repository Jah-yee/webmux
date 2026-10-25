package codeview

import (
	"context"
	"time"
)

func (s *Service) Check(ctx context.Context, id string, req CheckRequest) (Check, error) {
	r := Check{Snapshots: map[string]string{}, Directories: map[string]string{}, Changes: "unchanged", Checked: time.Now().UTC()}
	if len(req.Snapshots) > 40 || len(req.Directories) > 30 {
		return r, problem(ErrInvalid, "检查数量超过限制")
	}
	return withRoot(ctx, s, id, req.Context, func(b *binding) (Check, error) {
		views := map[string]*snapshot{}
		wanted := map[string]bool{}
		for _, sid := range req.Snapshots {
			v, err := s.get(id, sid)
			if err != nil {
				if errorKind(err) == ErrInvalid {
					return r, err
				}
				r.Snapshots[sid] = "expired"
				continue
			}
			if v.context != req.Context {
				return r, problem(ErrInvalid, "快照上下文不匹配")
			}
			views[sid] = v
			if v.diff != nil {
				wanted[v.diff.Change.ID] = true
			}
		}
		current := map[string]Change{}
		known := false
		if b.Diff && (req.Changes != "" || len(wanted) > 0) {
			version, err := b.scanChanges(ctx, func(c Change) error {
				if wanted[c.ID] {
					current[c.ID] = c
				}
				return nil
			})
			if err != nil {
				r.Changes = "unknown"
			} else {
				known = true
				if req.Changes != "" && req.Changes != version {
					r.Changes = "updated"
				}
			}
		}
		for _, sid := range req.Snapshots {
			v := views[sid]
			if v == nil {
				continue
			}
			state := "unchanged"
			if v.file != nil {
				d := v.file.Document
				if d.Source != "empty" {
					next, e := b.sourceFile(ctx, d.rawPath, d.Source)
					if e != nil {
						state = "unknown"
						if errorKind(e) == ErrNotFound {
							state = "missing"
						}
					} else if next.Version != d.Version || next.Kind != d.Kind {
						state = "updated"
					}
				}
			} else {
				if !known {
					state = "unknown"
				} else {
					found, exists := current[v.diff.Change.ID]
					if !exists {
						state = "updated"
					} else if found.Group != "staged" && v.diff.New.Source == "worktree" {
						next, e := b.read(found.NewPath)
						if e != nil {
							state = "unknown"
							if errorKind(e) == ErrNotFound {
								state = "missing"
							}
						} else if next.Version != v.diff.New.Version {
							state = "updated"
						}
					}
				}
			}
			r.Snapshots[sid] = state
		}
		for nid, version := range req.Directories {
			p, e := nodePath(nid)
			if e != nil {
				return r, e
			}
			next, e := b.listing(ctx, p, req.IncludeIgnored)
			state := "unchanged"
			if e != nil {
				state = "unknown"
			} else if next.Version != version {
				state = "updated"
			}
			r.Directories[nid] = state
		}
		return r, nil
	})
}
