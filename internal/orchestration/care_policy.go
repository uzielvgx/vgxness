package orchestration

func CAREActivation() (ActivationDecision, error) {
	return ActivationDecision{Level: ActivationStandard, Reasons: []string{"CARE is the default for non-exempt work"}}, nil
}

type Allocation struct {
	Level            ActivationLevel
	Roles            []CARERole
	IncludeGeneral   bool
	IncludeVerifier  bool
	RequiredAttempts int
}

func AllocationFor(level ActivationLevel, general bool) (Allocation, error) {
	a := Allocation{Level: level, IncludeGeneral: general, IncludeVerifier: true}
	switch level {
	case ActivationStandard:
		a.Roles = []CARERole{CAREReviewer}
		a.RequiredAttempts = 3
	case ActivationElevated:
		a.Roles = []CARERole{CAREReviewer, CARESpecialist}
		a.RequiredAttempts = 4
	case ActivationCritical:
		a.Roles = []CARERole{CAREReviewer, CARESpecialist, CAREChallenger}
		a.RequiredAttempts = 5
	default:
		return a, ErrCAREBlocked
	}
	return a, nil
}
