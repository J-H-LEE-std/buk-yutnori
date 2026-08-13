package board

// BukPlanner exposes the immutable canonical board facts needed by automatic
// Buk destination and target resolution.
type BukPlanner interface {
	FinishDistancePlanner
	FixedBukDestination() SpaceID
	BukCandidates() []SpaceID
	Node(id SpaceID) (Node, bool)
}
