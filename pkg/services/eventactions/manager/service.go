package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sync"
	"time"

	"github.com/grafana/grafana/pkg/api/routing"
	"github.com/grafana/grafana/pkg/infra/httpclient"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/infra/usagestats"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	"github.com/grafana/grafana/pkg/services/eventactions"
	"github.com/grafana/grafana/pkg/services/eventactions/api"
	"github.com/grafana/grafana/pkg/services/eventactions/database"
	"github.com/grafana/grafana/pkg/setting"
)

type EventActionsService struct {
	store eventactions.Store
	log   log.Logger
}

func ProvideEventActionsService(
	cfg *setting.Cfg,
	ac accesscontrol.AccessControl,
	routeRegister routing.RouteRegister,
	usageStats usagestats.Service,
	eventActionsStore eventactions.Store,
	eventService eventactions.EventsService,
	permissionService accesscontrol.EventActionPermissionsService,
) (*EventActionsService, error) {
	database.InitMetrics()
	s := &EventActionsService{
		store: eventActionsStore,
		log:   log.New("eventactions"),
	}

	s.log.Info("Registering event actions")

	if err := RegisterRoles(ac); err != nil {
		s.log.Error("Failed to register roles", "error", err)
	}

	usageStats.RegisterMetricsFunc(s.store.GetUsageMetrics)

	eventactionsAPI := api.NewEventActionsAPI(cfg, s, eventService, ac, routeRegister, s.store, permissionService)
	eventactionsAPI.RegisterAPIEndpoints()

	return s, nil
}

func (sa *EventActionsService) Run(ctx context.Context) error {
	sa.log.Debug("Started Event Action Metrics collection service")
	return sa.store.RunMetricsCollection(ctx)
}

func (sa *EventActionsService) CreateEventAction(ctx context.Context, orgID int64, form *eventactions.CreateEventActionForm) (*eventactions.EventActionDetailsDTO, error) {
	return sa.store.CreateEventAction(ctx, orgID, form)
}

func (sa *EventActionsService) DeleteEventAction(ctx context.Context, orgID, eventActionID int64) error {
	return sa.store.DeleteEventAction(ctx, orgID, eventActionID)
}

func (sa *EventActionsService) RetrieveEventActionByName(ctx context.Context, orgID int64, name string) (*eventactions.EventActionDetailsDTO, error) {
	return sa.store.RetrieveEventActionByName(ctx, orgID, name)
}

func (sa *EventActionsService) RetrieveEventActionsByRegisteredEvent(ctx context.Context, orgID int64, eventName string) ([]*eventactions.EventActionDetailsDTO, error) {
	return sa.store.RetrieveEventActionsByRegisteredEvent(ctx, orgID, eventName)
}

type EventsService struct {
	log     log.Logger
	store   eventactions.EventStore
	actions eventactions.Store
	client  *http.Client
}

func ProvideEventsService(store eventactions.EventStore, actionsStore eventactions.Store, httpClientProvider httpclient.Provider) (*EventsService, error) {
	logger := log.New("events")
	logger.Info("Registering events service")

	client, err := httpClientProvider.New()
	if err != nil {
		return nil, err
	}

	s := &EventsService{
		log:     logger,
		store:   store,
		actions: actionsStore,
		client:  client,
	}

	return s, nil
}

func (s *EventsService) Register(ctx context.Context, form *eventactions.RegisterEventForm) (*eventactions.EventDTO, error) {
	evt, err := s.store.CreateEvent(ctx, form)
	if err != nil {
		s.log.Error("creating event", "name", form.Name, "err", err)
		return nil, err
	}

	s.log.Info("event registered", "name", form.Name)
	return evt, nil
}

func (s *EventsService) ListEvents(ctx context.Context) ([]*eventactions.EventDTO, error) {
	return s.store.ListEvents(ctx)
}

func (s *EventsService) Unregister(ctx context.Context, eventName string) error {
	return s.store.DeleteEvent(ctx, eventName)
}

type runnerMetadata struct {
	Name  string `json:"name"`
	Lang  string `json:"lang"`
	Entry string `json:"entrypoint"`
}

func (s *EventsService) Publish(ctx context.Context, orgID int64, eventName string, eventPayload interface{}) error {
	actions, err := s.actions.RetrieveEventActionsByRegisteredEvent(ctx, orgID, eventName)
	if err != nil {
		s.log.Error("retrieving event actions by registered event", "err", err, "orgID", orgID, "event", eventName)
		return err
	}

	// TODO these values should be configurable
	const numWorkers = 3

	var wg sync.WaitGroup

	worker := func(jobs <-chan *eventactions.EventActionDetailsDTO) {
		defer wg.Done()
		wg.Add(1)

		for action := range jobs {
			if _, err := s.RunEventAction(ctx, action, eventName, eventPayload); err != nil {
				s.log.Error("running event action", "err", err, "action", action.Name, "event", eventName)
			}
		}
	}

	start := time.Now()

	jobs := make(chan *eventactions.EventActionDetailsDTO, len(actions))
	for w := 0; w < numWorkers; w++ {
		go worker(jobs)
	}
	for _, action := range actions {
		jobs <- action
	}
	close(jobs)

	wg.Wait()

	s.log.Info("event published successfully", "event", eventName, "orgID", orgID, "actions", len(actions), "workers", numWorkers, "duration", time.Since(start))

	return nil
}

type createRequestFunc func(eventName string, eventPayload interface{}, action *eventactions.EventActionDetailsDTO) (*http.Request, error)

func createRunnerRequest(eventName string, eventPayload interface{}, action *eventactions.EventActionDetailsDTO) (*http.Request, error) {
	metadata, err := json.Marshal(runnerMetadata{
		Name:  action.Name,
		Lang:  action.ScriptLanguage,
		Entry: "file1",
		// TODO missing entrypoint
	})
	if err != nil {
		return nil, fmt.Errorf("cannot serialize runner metadata: %w", err)
	}

	marshalledPayload, err := json.Marshal(eventPayload)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize event payload: %w", err)
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	scriptFile, err := w.CreateFormFile("file1", "file1")
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(scriptFile, action.Script); err != nil {
		return nil, err
	}

	metadataHeaders := make(textproto.MIMEHeader)
	metadataHeaders.Set("Content-Disposition", `form-data; name="metadata"`)
	metadataHeaders.Set("Content-Type", "application/json")
	metadataPart, err := w.CreatePart(metadataHeaders)
	if err != nil {
		return nil, err
	}
	if _, err := metadataPart.Write(metadata); err != nil {
		return nil, err
	}

	payloadHeaders := make(textproto.MIMEHeader)
	payloadHeaders.Set("Content-Disposition", `form-data; name="event"`)
	payloadHeaders.Set("Content-Type", "application/json")
	payloadPart, err := w.CreatePart(payloadHeaders)
	if err != nil {
		return nil, err
	}
	if _, err := payloadPart.Write(marshalledPayload); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	url, err := url.JoinPath(action.URL, "execute")
	if err != nil {
		return nil, fmt.Errorf("cannot create runner URL: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, &b)
	if err != nil {
		return nil, fmt.Errorf("cannot create runner request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+action.RunnerSecret)
	req.Header.Set("Content-Type", w.FormDataContentType())

	return req, nil
}

func createWebhookRequest(eventName string, eventPayload interface{}, action *eventactions.EventActionDetailsDTO) (*http.Request, error) {
	body, err := json.Marshal(eventactions.PublishEvent{
		EventName: eventName,
		OrgId:     action.OrgId,
		Payload:   eventPayload,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot serialize external webhook payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, action.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cannot create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (s *EventsService) RunEventAction(ctx context.Context, action *eventactions.EventActionDetailsDTO, eventName string, eventPayload interface{}) (*eventactions.RunResponse, error) {
	var createRequest createRequestFunc

	switch action.Type {
	case string(eventactions.ActionTypeCode):
		createRequest = createRunnerRequest

	case string(eventactions.ActionTypeWebhook):
		createRequest = createWebhookRequest
	}

	req, err := createRequest(eventName, eventPayload, action)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot perform request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read response body: %w", err)
	}

	return &eventactions.RunResponse{
		Code: response.StatusCode,
		Body: string(body),
	}, nil
}