package airgap_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAirgap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Airgap Suite")
}
