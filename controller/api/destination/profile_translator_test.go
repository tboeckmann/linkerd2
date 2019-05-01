package destination

import (
	"context"
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/pkg/profiles"
)

var (
	getButNotPrivate = &sp.RequestMatch{
		All: []*sp.RequestMatch{
			{
				Method: "GET",
			},
			{
				Not: &sp.RequestMatch{
					PathRegex: "/private/.*",
				},
			},
		},
	}

	pbGetButNotPrivate = &pb.RequestMatch{
		Match: &pb.RequestMatch_All{
			All: &pb.RequestMatch_Seq{
				Matches: []*pb.RequestMatch{
					{
						Match: &pb.RequestMatch_Method{
							Method: &httpPb.HttpMethod{
								Type: &httpPb.HttpMethod_Registered_{
									Registered: httpPb.HttpMethod_GET,
								},
							},
						},
					},
					{
						Match: &pb.RequestMatch_Not{
							Not: &pb.RequestMatch{
								Match: &pb.RequestMatch_Path{
									Path: &pb.PathMatch{
										Regex: "/private/.*",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	login = &sp.RequestMatch{
		PathRegex: "/login",
	}

	pbLogin = &pb.RequestMatch{
		Match: &pb.RequestMatch_Path{
			Path: &pb.PathMatch{
				Regex: "/login",
			},
		},
	}

	fiveXX = &sp.ResponseMatch{
		Status: &sp.Range{
			Min: 500,
			Max: 599,
		},
	}

	pbFiveXX = &pb.ResponseMatch{
		Match: &pb.ResponseMatch_Status{
			Status: &pb.HttpStatusRange{
				Min: 500,
				Max: 599,
			},
		},
	}

	fiveXXfourTwenty = &sp.ResponseMatch{
		Any: []*sp.ResponseMatch{
			fiveXX,
			{
				Status: &sp.Range{
					Min: 420,
					Max: 420,
				},
			},
		},
	}

	pbFiveXXfourTwenty = &pb.ResponseMatch{
		Match: &pb.ResponseMatch_Any{
			Any: &pb.ResponseMatch_Seq{
				Matches: []*pb.ResponseMatch{
					pbFiveXX,
					{
						Match: &pb.ResponseMatch_Status{
							Status: &pb.HttpStatusRange{
								Min: 420,
								Max: 420,
							},
						},
					},
				},
			},
		},
	}

	route1 = &sp.RouteSpec{
		Name:      "route1",
		Condition: getButNotPrivate,
		ResponseClasses: []*sp.ResponseClass{
			{
				Condition: fiveXX,
				IsFailure: true,
			},
		},
	}

	pbRoute1 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route1",
		},
		Condition: pbGetButNotPrivate,
		ResponseClasses: []*pb.ResponseClass{
			{
				Condition: pbFiveXX,
				IsFailure: true,
			},
		},
		Timeout: &duration.Duration{
			Seconds: 10,
		},
	}

	route2 = &sp.RouteSpec{
		Name:      "route2",
		Condition: login,
		ResponseClasses: []*sp.ResponseClass{
			{
				Condition: fiveXXfourTwenty,
				IsFailure: true,
			},
		},
	}

	pbRoute2 = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "route2",
		},
		Condition: pbLogin,
		ResponseClasses: []*pb.ResponseClass{
			{
				Condition: pbFiveXXfourTwenty,
				IsFailure: true,
			},
		},
		Timeout: &duration.Duration{
			Seconds: 10,
		},
	}

	profile = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				route1,
				route2,
			},
		},
	}

	pbProfile = &pb.DestinationProfile{
		Routes: []*pb.Route{
			pbRoute1,
			pbRoute2,
		},
		RetryBudget: &profiles.DefaultRetryBudget,
	}

	defaultPbProfile = &pb.DestinationProfile{
		Routes:      []*pb.Route{},
		RetryBudget: &profiles.DefaultRetryBudget,
	}

	multipleRequestMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "multipleRequestMatches",
					Condition: &sp.RequestMatch{
						Method:    "GET",
						PathRegex: "/my/path",
					},
				},
			},
		},
	}

	pbRequestMatchAll = &pb.DestinationProfile{
		Routes: []*pb.Route{
			{
				Condition: &pb.RequestMatch{
					Match: &pb.RequestMatch_All{
						All: &pb.RequestMatch_Seq{
							Matches: []*pb.RequestMatch{
								{
									Match: &pb.RequestMatch_Method{
										Method: &httpPb.HttpMethod{
											Type: &httpPb.HttpMethod_Registered_{
												Registered: httpPb.HttpMethod_GET,
											},
										},
									},
								},
								{
									Match: &pb.RequestMatch_Path{
										Path: &pb.PathMatch{
											Regex: "/my/path",
										},
									},
								},
							},
						},
					},
				},
				MetricsLabels: map[string]string{
					"route": "multipleRequestMatches",
				},
				ResponseClasses: []*pb.ResponseClass{},
				Timeout: &duration.Duration{
					Seconds: 10,
				},
			},
		},
		RetryBudget: &profiles.DefaultRetryBudget,
	}

	notEnoughRequestMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{},
				},
			},
		},
	}

	multipleResponseMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "multipleResponseMatches",
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 400,
									Max: 499,
								},
								Not: &sp.ResponseMatch{
									Status: &sp.Range{
										Min: 404,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	pbResponseMatchAll = &pb.DestinationProfile{
		Routes: []*pb.Route{
			{
				Condition: &pb.RequestMatch{
					Match: &pb.RequestMatch_Method{
						Method: &httpPb.HttpMethod{
							Type: &httpPb.HttpMethod_Registered_{
								Registered: httpPb.HttpMethod_GET,
							},
						},
					},
				},
				MetricsLabels: map[string]string{
					"route": "multipleResponseMatches",
				},
				ResponseClasses: []*pb.ResponseClass{
					{
						Condition: &pb.ResponseMatch{
							Match: &pb.ResponseMatch_All{
								All: &pb.ResponseMatch_Seq{
									Matches: []*pb.ResponseMatch{
										{
											Match: &pb.ResponseMatch_Status{
												Status: &pb.HttpStatusRange{
													Min: 400,
													Max: 499,
												},
											},
										},
										{
											Match: &pb.ResponseMatch_Not{
												Not: &pb.ResponseMatch{
													Match: &pb.ResponseMatch_Status{
														Status: &pb.HttpStatusRange{
															Min: 404,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Timeout: &duration.Duration{
					Seconds: 10,
				},
			},
		},
		RetryBudget: &profiles.DefaultRetryBudget,
	}

	oneSidedStatusRange = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 200,
								},
							},
						},
					},
				},
			},
		},
	}

	invalidStatusRange = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 201,
									Max: 200,
								},
							},
						},
					},
				},
			},
		},
	}

	notEnoughResponseMatches = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Condition: &sp.RequestMatch{
						Method: "GET",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{},
						},
					},
				},
			},
		},
	}

	routeWithTimeout = &sp.RouteSpec{
		Name:            "routeWithTimeout",
		Condition:       login,
		ResponseClasses: []*sp.ResponseClass{},
		Timeout:         "200ms",
	}

	profileWithTimeout = &sp.ServiceProfile{
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				routeWithTimeout,
			},
		},
	}

	pbRouteWithTimeout = &pb.Route{
		MetricsLabels: map[string]string{
			"route": "routeWithTimeout",
		},
		Condition:       pbLogin,
		ResponseClasses: []*pb.ResponseClass{},
		Timeout: &duration.Duration{
			Nanos: 200000000, // 200ms
		},
	}

	pbProfileWithTimeout = &pb.DestinationProfile{
		Routes: []*pb.Route{
			pbRouteWithTimeout,
		},
		RetryBudget: &profiles.DefaultRetryBudget,
	}
)

func TestProfileListener(t *testing.T) {
	t.Run("Sends update", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(profile)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, pbProfile) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbProfile, actualPbProfile)
		}
	})

	t.Run("Request match with more than one field becomes ALL", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(multipleRequestMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, pbRequestMatchAll) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbRequestMatchAll, actualPbProfile)
		}
	})

	t.Run("Ignores request match without any fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(notEnoughRequestMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Response match with more than one field becomes ALL", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(multipleResponseMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, pbResponseMatchAll) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbResponseMatchAll, actualPbProfile)
		}
	})

	t.Run("Ignores response match without any fields", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(notEnoughResponseMatches)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Ignores response match with invalid status range", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(invalidStatusRange)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 0 {
			t.Fatalf("Expecting [0] profiles, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("Sends update for one sided status range", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(oneSidedStatusRange)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
	})

	t.Run("It returns when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetProfileServer := &mockDestinationGetProfileServer{
			profilesReceived: []*pb.DestinationProfile{},
			mockDestinationServer: mockDestinationServer{
				contextToReturn: context,
			},
		}
		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		completed := make(chan bool)
		go func() {
			<-listener.ClientClose()
			completed <- true
		}()

		cancelFn()

		c := <-completed

		if !c {
			t.Fatalf("Expected function to be completed after the cancel()")
		}
	})

	t.Run("Sends empty update", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(nil)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, defaultPbProfile) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", defaultPbProfile, actualPbProfile)
		}
	})

	t.Run("Sends update with custom timeout", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{profilesReceived: []*pb.DestinationProfile{}}

		listener := &profileListener{
			stream: mockGetProfileServer,
		}

		listener.Update(profileWithTimeout)

		numProfiles := len(mockGetProfileServer.profilesReceived)
		if numProfiles != 1 {
			t.Fatalf("Expecting [1] profile, got [%d]. Updates: %v", numProfiles, mockGetProfileServer.profilesReceived)
		}
		actualPbProfile := mockGetProfileServer.profilesReceived[0]
		if !reflect.DeepEqual(actualPbProfile, pbProfileWithTimeout) {
			t.Fatalf("Expected profile sent to be [%v] but was [%v]", pbProfileWithTimeout, actualPbProfile)
		}
	})
}
