package portalauth

// AmericanLetterGrade converts a percentage to an American letter grade.
func AmericanLetterGrade(percent float64) string {
	switch {
	case percent >= 93:
		return "A"
	case percent >= 90:
		return "A-"
	case percent >= 87:
		return "B+"
	case percent >= 83:
		return "B"
	case percent >= 80:
		return "B-"
	case percent >= 77:
		return "C+"
	case percent >= 73:
		return "C"
	case percent >= 70:
		return "C-"
	case percent >= 67:
		return "D+"
	case percent >= 63:
		return "D"
	case percent >= 60:
		return "D-"
	default:
		return "F"
	}
}
