package resource

import "errors"

var (
	ErrDeliveryPodOwnershipUnverified = errors.New("delivery pod ownership unverified")
	ErrDeliveryPodObservationLimit    = errors.New("delivery pod observation limit")
)
