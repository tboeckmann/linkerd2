package watcher

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/intstr"
	"regexp"
)

var k8sSvcNameRE = regexp.MustCompile("^(?i)([^.]+)\\.([^.]+)\\.svc\\.cluster\\.local\\.?(?::(\\d+))?$")

type (
	ID struct {
		Namespace string
		Name      string
	}
	ServiceID = ID
	PodID     = ID
	ProfileID = ID

	Port      = uint32
	namedPort = intstr.IntOrString
)

func (i ID) String() string {
	return fmt.Sprintf("%s/%s", i.Namespace, i.Name)
}
