package watcher

import (
	"reflect"
	"testing"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

type bufferingProfileListener struct {
	profiles []*sp.ServiceProfile
}

func newBufferingProfileListener() bufferingProfileListener {
	return bufferingProfileListener{
		profiles: []*sp.ServiceProfile{},
	}
}

func (bpl bufferingProfileListener) Update(profile *sp.ServiceProfile) {
	bpl.profiles = append(bpl.profiles, profile)
}

func TestProfileWatcher(t *testing.T) {
	for _, tt := range []struct {
		name             string
		k8sConfigs       []string
		service          ProfileID
		expectedProfiles []*sp.ServiceProfileSpec
	}{
		{
			name: "service profile",
			k8sConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar.ns.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: "/x/y/z"
    responseClasses:
    - condition:
        status:
          min: 500
      isFailure: true`,
			},
			service: ProfileID{Namespace: "linkerd", Name: "foobar.ns.svc.cluster.local"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				{
					Routes: []*sp.RouteSpec{
						{
							Condition: &sp.RequestMatch{
								PathRegex: "/x/y/z",
							},
							ResponseClasses: []*sp.ResponseClass{
								{
									Condition: &sp.ResponseMatch{
										Status: &sp.Range{
											Min: 500,
										},
									},
									IsFailure: true,
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "service without profile",
			k8sConfigs: []string{},
			service:    ProfileID{Namespace: "linkerd", Name: "foobar.ns"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				nil,
			},
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewProfileWatcher(k8sAPI, logging.WithFields(logging.Fields{"test": t.Name}))

			k8sAPI.Sync()

			listener := newBufferingProfileListener()

			watcher.Subscribe(tt.service, listener)

			actualProfiles := make([]*sp.ServiceProfileSpec, 0)

			for _, profile := range listener.profiles {
				if profile == nil {
					actualProfiles = append(actualProfiles, nil)
				} else {
					actualProfiles = append(actualProfiles, &profile.Spec)
				}
			}

			if !reflect.DeepEqual(actualProfiles, tt.expectedProfiles) {
				t.Fatalf("Expected profiles %v, got %v", tt.expectedProfiles, listener.profiles)
			}
		})
	}
}
