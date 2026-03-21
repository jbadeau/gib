package gib

// ProgressPhase identifies a build lifecycle stage.
type ProgressPhase string

const (
	PhaseContainerizing ProgressPhase = "CONTAINERIZING"
	PhasePullingBase    ProgressPhase = "PULLING_BASE"
	PhaseBuildingLayer  ProgressPhase = "BUILDING_LAYER"
	PhaseBuildingImage  ProgressPhase = "BUILDING_IMAGE"
	PhaseWriting        ProgressPhase = "WRITING"
	PhaseFinalizing     ProgressPhase = "FINALIZING"
)

// ProgressEvent represents a build progress update.
type ProgressEvent struct {
	Phase   ProgressPhase
	Message string
}

// ProgressCallback is a function that receives build progress updates.
type ProgressCallback func(ProgressEvent)
