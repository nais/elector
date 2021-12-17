package logging

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/sirupsen/logrus"
)

type Logrus2Logr struct {
	Logger logrus.FieldLogger
	name   string
}

func (l *Logrus2Logr) Enabled() bool {
	return l.Logger != nil
}

func (l *Logrus2Logr) Info(msg string, keysAndValues ...interface{}) {
	fields := makeFields(keysAndValues)
	l.Logger.WithFields(fields).Info(msg)
}

func (l *Logrus2Logr) Error(err error, msg string, keysAndValues ...interface{}) {
	fields := makeFields(keysAndValues)
	l.Logger.WithFields(fields).Error(fmt.Errorf("%s: %w", msg, err))
}

func (l *Logrus2Logr) V(_ int) logr.Logger {
	return l
}

func (l *Logrus2Logr) WithValues(keysAndValues ...interface{}) logr.Logger {
	fields := makeFields(keysAndValues)
	return &Logrus2Logr{Logger: l.Logger.WithFields(fields)}
}

func (l *Logrus2Logr) WithName(name string) logr.Logger {
	if l.name != "" {
		name = fmt.Sprintf("%s.%s", l.name, name)
	}
	return &Logrus2Logr{Logger: l.Logger.WithField("logger_name", name), name: name}
}

func makeFields(keysAndValues []interface{}) logrus.Fields {
	fields := logrus.Fields{}
	for i, value := range keysAndValues {
		if i%2 == 1 {
			key := keysAndValues[i-1].(string)
			fields[key] = value
		}
	}
	return fields
}
