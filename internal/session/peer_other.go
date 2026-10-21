//go:build !linux && !darwin

package session

func hasSessionMarker(int) bool                            { return false }
func insideTree(processTable, int, int, map[int]bool) bool { return false }
