package official

import (
	"context"
	"fmt"
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

	Context("sse api", func() {
		var w *httptest.ResponseRecorder

		BeforeEach(func() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
			DeferCleanup(cancel)

			w = httptest.NewRecorder()
			o.lastResult = result{
				Name:       "last result",
				LastUpdate: "then",
			}
		})

		It("should return initial election result", func() {
			go o.sseHandler(ctx)(w, nil)
			time.Sleep(10 * time.Millisecond)

			line, err := w.Body.ReadString('\n')
			Expect(err).ToNot(HaveOccurred())
			Expect(line).To(HavePrefix("data: "))
			data := line[6:]
			Expect(data).To(MatchJSON(`{"name":"last result","last_update":"then"}`))
		})

		It("should continue to update election results", func() {
			go o.sseHandler(ctx)(w, nil)
			time.Sleep(10 * time.Millisecond)

			line, err := w.Body.ReadString('\n')
			Expect(err).ToNot(HaveOccurred())
			Expect(line).To(HavePrefix("data: "))
			data := line[6:]
			Expect(data).To(MatchJSON(`{"name":"last result","last_update":"then"}`))
			Expect(w.Body.ReadString('\n')).To(Equal("\n"))

			for _, result := range []string{"first result", "second result", "third result"} {
				electionResults <- result
				time.Sleep(10 * time.Millisecond)

				line, err := w.Body.ReadString('\n')
				Expect(err).ToNot(HaveOccurred())
				Expect(line).To(HavePrefix("data: "))
				data := line[6:]
				Expect(data).To(ContainUnorderedJSON(fmt.Sprintf(`{"name":"%s"}`, result)))
				Expect(w.Body.ReadString('\n')).To(Equal("\n"))
			}
		})
	})
})
