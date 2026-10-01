package upstream_test

import "net/url"

type targetAcquirer interface {
	Acquire() (url.URL, func())
}

func acquireTarget(acquirer targetAcquirer) url.URL {
	target, release := acquirer.Acquire()
	release()
	return target
}
