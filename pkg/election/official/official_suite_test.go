package official_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOfficial(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Official Suite")
}
