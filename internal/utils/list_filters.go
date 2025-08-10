package utils

type FilterMode string

const (
	FilterModeContains  FilterMode = "contains"
	FilterModeStartWhit FilterMode = "start_with"
	FilterModeEndWith   FilterMode = "end_with"
)

type Filter struct {
	Value string     `json:"value"`
	Mode  FilterMode `json:"mode"`
}

type SortByDirection string

const (
	SortByDirectionAsc  SortByDirection = "asc"
	SortByDirectionDesc SortByDirection = "desc"
)
