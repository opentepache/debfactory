package installtest

import (
	"errors"
	"fmt"
	"os/exec"
)

// CheckVersions comprueba con `dpkg --compare-versions` del host que
// <upstream>-0~pav<N> es menor que la oficial hipotética <upstream>-1 y, si
// N > 1, mayor que <upstream>-0~pav<N-1> (FR-024, constitución VI).
func CheckVersions(upstream string, pav int) error {
	v := fmt.Sprintf("%s-0~pav%d", upstream, pav)
	official := upstream + "-1"
	if ok, err := CompareVersions(v, "lt", official); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s no es menor que %s", v, official)
	}
	if pav > 1 {
		prev := fmt.Sprintf("%s-0~pav%d", upstream, pav-1)
		if ok, err := CompareVersions(v, "gt", prev); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%s no es mayor que %s", v, prev)
		}
	}
	return nil
}

// CompareVersions ejecuta `dpkg --compare-versions a op b` en el host.
func CompareVersions(a, op, b string) (bool, error) {
	err := exec.Command("dpkg", "--compare-versions", a, op, b).Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		return false, nil
	}
	return false, fmt.Errorf("dpkg --compare-versions %s %s %s: %v", a, op, b, err)
}
