package official

import (
	"context"
	. "github.com/benjamintf1/unmarshalledmatchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"io"
	"net/http/httptest"
	"time"
)

var _ = Describe("Official", func() {
	var ctx context.Context
	var o *official
	var logger logrus.FieldLogger
	var electionResults chan string

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)

		logger = logrus.New()
		electionResults = make(chan string)
		o = &official{
			Logger:          logger,
			ElectionResults: electionResults,
		}

		go func() {
			_ = o.run(ctx)
		}()
	})

	Context("simple api", func() {
		var w *httptest.ResponseRecorder

		BeforeEach(func() {
			w = httptest.NewRecorder()
			o.lastResult = result{
				Name:       "last result",
				LastUpdate: "then",
			}
		})

		It("should return initial election result", func() {
			o.leaderHandler(w, nil)

			res := w.Result()
			defer res.Body.Close()

			Expect(res.StatusCode).To(Equal(200))
			Expect(res.Header.Get("Content-Type")).To(Equal("application/json"))
			Expect(res.Header.Get("Cache-Control")).To(Equal("no-cache"))
			Expect(io.ReadAll(res.Body)).To(MatchJSON(`{"name":"last result","last_update":"then"}`))
		})

		It("should return election result update", func() {
			electionResults <- "new result"
			time.Sleep(10 * time.Millisecond)

			o.leaderHandler(w, nil)

			res := w.Result()
			defer res.Body.Close()

			Expect(res.StatusCode).To(Equal(200))
			Expect(res.Header.Get("Content-Type")).To(Equal("application/json"))
			Expect(res.Header.Get("Cache-Control")).To(Equal("no-cache"))
			Expect(io.ReadAll(res.Body)).To(ContainUnorderedJSON(`{"name":"new result"}`))
		})
	})
})
