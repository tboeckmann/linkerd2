package watcher

import (
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	splisters "github.com/linkerd/linkerd2/controller/gen/client/listers/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"k8s.io/client-go/tools/cache"
)

type (
	// profileWatcher watches all service profiles in the Kubernetes cluster.
	// Listeners can subscribe to a particular profile and profileWatcher will
	// publish the service profile and all future changes for that profile.
	ProfileWatcher struct {
		profileLister splisters.ServiceProfileLister
		profiles      map[ProfileID]*profilePublisher
		profilesLock  sync.RWMutex
	}

	profilePublisher struct {
		profile   *sp.ServiceProfile
		listeners []ProfileUpdateListener
		mutex     sync.Mutex
	}

	ProfileUpdateListener interface {
		Update(profile *sp.ServiceProfile)
	}
)

func NewProfileWatcher(k8sAPI *k8s.API) *ProfileWatcher {
	watcher := &ProfileWatcher{
		profileLister: k8sAPI.SP().Lister(),
		profiles:      make(map[ProfileID]*profilePublisher),
	}

	k8sAPI.SP().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addProfile,
			UpdateFunc: watcher.updateProfile,
			DeleteFunc: watcher.deleteProfile,
		},
	)

	return watcher
}

// func (p *profileWatcher) resolveProfiles(
// 	name authority,
// 	context string,
// 	listener profileUpdateListener,
// ) error {
// 	subscriptions := map[profileID]profileUpdateListener{}

// 	primaryListener, secondaryListener := newFallbackProfileListener(listener)

// 	if ns := nsFromToken(context); ns != "" {
// 		clientProfileID := profileID{
// 			namespace: ns,
// 			name:      host,
// 		}

// 		err := k.profileWatcher.subscribeToProfile(clientProfileID, primaryListener)
// 		if err != nil {
// 			log.Error(err)
// 			return err
// 		}
// 		subscriptions[clientProfileID] = primaryListener
// 	}

// 	if err == nil && serviceID != nil {
// 		serverProfileID := profileID{
// 			namespace: serviceID.namespace,
// 			name:      host,
// 		}

// 		err := p.subscribeToProfile(serverProfileID, secondaryListener)
// 		if err != nil {
// 			log.Error(err)
// 			return err
// 		}
// 		subscriptions[serverProfileID] = secondaryListener
// 	}

// 	select {
// 	case <-listener.ClientClose():
// 		for id, listener := range subscriptions {
// 			err = p.unsubscribeToProfile(id, listener)
// 			if err != nil {
// 				return err
// 			}
// 		}
// 		return nil
// 	}

// 	return nil
// }

// func nsFromToken(ctx string) string {
// 	// ns:<namespace>
// 	parts := strings.Split(ctx, ":")
// 	if len(parts) == 2 && parts[0] == "ns" {
// 		return parts[1]
// 	}

// 	return ""

// 	// if proxyNS != "" {
// 	// 	log.Debugf("Looking up profile given context: ns:%s", proxyNS)
// 	// }
// }

// // Close all open streams on shutdown
// func (p *profileWatcher) stop() {
// 	p.profilesLock.Lock()
// 	defer p.profilesLock.Unlock()

// 	for _, profile := range p.profiles {
// 		profile.unsubscribeAll()
// 	}
// }

//////////////////////
/// ProfileWatcher ///
//////////////////////

func (pw *ProfileWatcher) Subscribe(name ProfileID, listener ProfileUpdateListener) {
	pw.profilesLock.Lock()

	publisher, ok := pw.profiles[name]
	if !ok {
		profile, err := pw.profileLister.ServiceProfiles(name.Namespace).Get(name.Name)
		if err != nil {
			profile = nil
		}

		publisher = newProfilePublisher(profile)
		pw.profiles[name] = publisher
	}
	pw.profilesLock.Unlock()

	publisher.subscribe(listener)
}

func (pw *ProfileWatcher) Unsubscribe(name ProfileID, listener ProfileUpdateListener) {
	pw.profilesLock.Lock()
	defer pw.profilesLock.Unlock()

	publisher, ok := pw.profiles[name]
	if ok {
		publisher.unsubscribe(listener)
	}
}

func (pw *ProfileWatcher) addProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := ProfileID{
		Namespace: profile.Namespace,
		Name:      profile.Name,
	}

	pw.profilesLock.RLock()
	publisher, ok := pw.profiles[id]
	if !ok {
		publisher = newProfilePublisher(profile)
		pw.profiles[id] = publisher

	}
	pw.profilesLock.RUnlock()

	publisher.update(profile)
}

func (pw *ProfileWatcher) updateProfile(old interface{}, new interface{}) {
	pw.addProfile(new)
}

func (pw *ProfileWatcher) deleteProfile(obj interface{}) {
	profile := obj.(*sp.ServiceProfile)
	id := ProfileID{
		Namespace: profile.Namespace,
		Name:      profile.Name,
	}

	publisher, ok := pw.profiles[id]
	if ok {
		publisher.update(nil)
	}
}

////////////////////////
/// profilePublisher ///
////////////////////////

func newProfilePublisher(profile *sp.ServiceProfile) *profilePublisher {
	return &profilePublisher{
		profile:   profile,
		listeners: make([]ProfileUpdateListener, 0),
	}
}

func (pp *profilePublisher) subscribe(listener ProfileUpdateListener) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	pp.listeners = append(pp.listeners, listener)
	listener.Update(pp.profile)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (pp *profilePublisher) unsubscribe(listener ProfileUpdateListener) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	for i, item := range pp.listeners {
		if item == listener {
			// delete the item from the slice
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			return
		}
	}
}

func (pp *profilePublisher) update(profile *sp.ServiceProfile) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	pp.profile = profile
	for _, listener := range pp.listeners {
		listener.Update(profile)
	}
}
