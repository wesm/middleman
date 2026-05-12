package dbtest

import (
	"sync"
	"testing"
)

func resetTemplateForTest(t *testing.T) {
	t.Helper()
	templateCache = &templateState{once: sync.Once{}}
}

func templateBuildCountForTest() int {
	return int(templateCache.buildCount.Load())
}
