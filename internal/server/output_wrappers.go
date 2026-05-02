package server

type bodyOutput[T any] struct {
	Body T
}

type createdOutput[T any] struct {
	Status int `status:"201"`
	Body   T
}

type acceptedBodyOutput[T any] struct {
	Status int `status:"202"`
	Body   T
}

type okStatusOutput struct {
	Status int `status:"200"`
}

type acceptedStatusOutput struct {
	Status int `status:"202"`
}
