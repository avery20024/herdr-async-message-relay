package claudeguard

import "errors"

func (s State) validate() error {
	switch s.Phase {
	case "ready", "ended":
		if s.Session == "" || !panePattern.MatchString(s.Pane) || s.Pending == nil {
			return errors.New("invalid guard state identity")
		}
	case "unknown":
		if (s.Session == "") != (s.Pane == "") {
			return errors.New("invalid unknown guard identity")
		}
	default:
		return errors.New("invalid guard state phase")
	}
	if len(s.Session) > 256 || s.UpdatedAt <= 0 || len(s.Pending) > 100 {
		return errors.New("invalid guard state bounds")
	}
	for id, pending := range s.Pending {
		if id == "" || len(id) > 256 || !pending {
			return errors.New("invalid pending question")
		}
	}
	return nil
}
