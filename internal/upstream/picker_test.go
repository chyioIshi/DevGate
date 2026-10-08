package upstream_test

import "net/url"

type targetAcquirer interface {
	Acquire() (url.URL, func(), error)
}

func acquireTarget(acquirer targetAcquirer) url.URL {
	target, release, err := acquirer.Acquire()
	if err != nil {
		panic(err)
	}
	release()
	return target
}
