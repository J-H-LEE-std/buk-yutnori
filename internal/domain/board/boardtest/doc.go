// Package boardtest provides reusable conformance checks for board planners.
//
// Every ForwardPlanner implementation must pass CheckForwardPlanner against
// the canonical board.Graph before it can be used by the authoritative match
// domain.
package boardtest
