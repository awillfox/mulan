package domain

const (
	SelectionSingleRequired = "single_required"
	SelectionSingleOptional = "single_optional"
	SelectionMulti          = "multi"
)

func ValidSelectionMode(m string) bool {
	switch m {
	case SelectionSingleRequired, SelectionSingleOptional, SelectionMulti:
		return true
	}
	return false
}
