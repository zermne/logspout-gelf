package gelf

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"time"
        "io/ioutil"
	"github.com/Graylog2/go-gelf/gelf"
	"github.com/gliderlabs/logspout/router"
)

var hostname string

func getopt(name, dfault string) string {
	value := os.Getenv(name)
	if value == "" {
		value = dfault
	}
	return value
}

func getHostname() string {
	content, err := ioutil.ReadFile("/etc/host_hostname")
	if err == nil && len(content) > 0 {
		hostname = strings.TrimRight(string(content), "\r\n")
	} else {
		log.Println("getHostname err:", err)
		hostname = getopt("K8S_HOSTNAME", "{{.Container.Config.Hostname}}")
	}
	return hostname
}

func init() {
	hostname = getHostname()
	router.AdapterFactories.Register(NewGelfAdapter, "gelf")
}

// GelfAdapter is an adapter that streams UDP JSON to Graylog
type GelfAdapter struct {
	writer *gelf.Writer
	route  *router.Route
}

// NewGelfAdapter creates a GelfAdapter with UDP as the default transport.
func NewGelfAdapter(route *router.Route) (router.LogAdapter, error) {
	_, found := router.AdapterTransports.Lookup(route.AdapterTransport("udp"))
	if !found {
		return nil, errors.New("unable to find adapter: " + route.Adapter)
	}

	gelfWriter, err := gelf.NewWriter(route.Address)
	if err != nil {
		return nil, err
	}

	return &GelfAdapter{
		route:  route,
		writer: gelfWriter,
	}, nil
}

// Stream implements the router.LogAdapter interface.
func (a *GelfAdapter) Stream(logstream chan *router.Message) {
	for message := range logstream {
		m := &GelfMessage{message}
		level := gelf.LOG_INFO
		if m.Source == "stderr" {
			level = gelf.LOG_ERR
		}
		extra, err := m.getExtraFields()
		if err != nil {
			log.Println("Graylog:", err)
			continue
		}

		msg := gelf.Message{
			Version:  "1.1",
			Host:     hostname,
			Short:    m.Message.Data,
			TimeUnix: float64(m.Message.Time.UnixNano()/int64(time.Millisecond)) / 1000.0,
			Level:    level,
			RawExtra: extra,
		}
		// 	ContainerId:    m.Container.ID,
		// 	ContainerImage: m.Container.Config.Image,
		// 	ContainerName:  m.Container.Name,
		// }

		// here be message write.
		if err := a.writer.WriteMessage(&msg); err != nil {
			log.Println("Graylog:", err)
			continue
		}
	}
}

type GelfMessage struct {
	*router.Message
}

func (m GelfMessage) getExtraFields() (json.RawMessage, error) {

	extra := map[string]interface{}{
		"_container_id":   m.Container.ID,
		"_container_name": m.Container.Name[1:], // might be better to use strings.TrimLeft() to remove the first /
		"_image_id":       m.Container.Image,
		"_image_name":     m.Container.Config.Image,
		"_command":        strings.Join(m.Container.Config.Cmd[:], " "),
		"_created":        m.Container.Created,
		"_k8s_namespace":  m.Container.Config.Labels["io.kubernetes.pod.namespace"],
		"_k8s_pod_name":   m.Container.Config.Labels["io.kubernetes.pod.name"],
		"_k8s_pod_uid":    m.Container.Config.Labels["io.kubernetes.pod.uid"],
		"_k8s_container_name":    m.Container.Config.Labels["io.kubernetes.container.name"],
	}
	for name, label := range m.Container.Config.Labels {
		if len(name) > 5 && strings.ToLower(name[0:5]) == "gelf_" {
			extra[name[4:]] = label
		}
	}
	swarmnode := m.Container.Node
	if swarmnode != nil {
		extra["_swarm_node"] = swarmnode.Name
	}

	rawExtra, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	return rawExtra, nil
}
