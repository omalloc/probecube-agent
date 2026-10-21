package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/wire"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

var ProviderSet = wire.NewSet(NewClient, NewPublisher, NewSubscriber)

type Ready interface {
	HasReady() bool
}

type Header = nats.Header
type Handler func(ctx context.Context, header Header, payload []byte) error

type Publisher interface {
	Ready

	Publish(ctx context.Context, subject string, payload proto.Message) error
	PublishWithHeaders(ctx context.Context, subject string, header Header, payload proto.Message) error
	RequestWithHeaders(ctx context.Context, subject string, h Header, req proto.Message, reply proto.Message) error
}

type Subscriber interface {
	Ready

	// Subscribe returns an error when the subscription could not be created, so
	// callers can fail fast instead of running without their task subscription.
	//
	// Note that NATS reports permission violations asynchronously: a broker that
	// rejects the subject surfaces the error through the connection's
	// ErrorHandler rather than through this return value.
	Subscribe(ctx context.Context, subject string, handler Handler) error
	Unsubscribe(ctx context.Context, subject string)
}

type natsPublisher struct {
	conn *Client
}

func NewPublisher(conn *Client) Publisher {
	return &natsPublisher{conn: conn}
}

func (r *natsPublisher) Publish(ctx context.Context, subject string, payload proto.Message) error {
	return r.PublishWithHeaders(ctx, subject, nil, payload)
}

func (r *natsPublisher) PublishWithHeaders(ctx context.Context, subject string, header Header, payload proto.Message) error {
	if header == nil {
		header = make(Header)
	}

	msg := &nats.Msg{
		Subject: subject,
		Header:  header,
		Data:    nil,
	}

	if payload != nil {
		data, err := proto.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal proto: %w", err)
		}
		msg.Data = data
	}

	msg.Header.Set("content-type", "application/protobuf")
	msg.Header.Set("schema-version", "v1")
	msg.Header.Set("sn", r.conn.sn)

	if err := r.conn.PublishMsg(msg); err != nil {
		return fmt.Errorf("publish msg: %w", err)
	}
	return nil
}

func (r *natsPublisher) Request(ctx context.Context, subject string, req proto.Message, reply proto.Message) error {
	return r.RequestWithHeaders(ctx, subject, nil, req, reply)
}

func (r *natsPublisher) RequestWithHeaders(ctx context.Context, subject string, h Header, req, reply proto.Message) error {
	if h == nil {
		h = make(Header)
	}

	h.Add("sn", r.conn.sn)

	reqMsg := &nats.Msg{
		Subject: subject,
		Header:  h,
		Data:    nil,
	}

	if req != nil {
		data, err := proto.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal proto request: %w", err)
		}
		reqMsg.Data = data
	}

	replyMsg, err := r.conn.RequestMsgWithContext(
		ctx,
		reqMsg,
	)
	if err != nil {
		return fmt.Errorf("request %s: %w", subject, err)
	}

	if replyMsg.Data != nil && reply != nil {
		if err := proto.Unmarshal(replyMsg.Data, reply); err != nil {
			return fmt.Errorf("unmarshal proto reply: %w", err)
		}
	}

	return nil
}

func (r *natsPublisher) HasReady() bool {
	return r.conn.Ready()
}

type natsSubscriber struct {
	log  *slog.Logger
	conn *Client

	mu       sync.Mutex
	subjects map[string]*nats.Subscription
}

func NewSubscriber(conn *Client, logger *slog.Logger) Subscriber {
	return &natsSubscriber{log: logger, conn: conn, subjects: make(map[string]*nats.Subscription)}
}

func (r *natsSubscriber) Subscribe(ctx context.Context, subject string, handler Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if subscribe, ok := r.subjects[subject]; ok {
		// Unsubscribe the previous incarnation before replacing it so the map
		// never holds a stale entry.
		if err := subscribe.Unsubscribe(); err != nil {
			r.log.WarnContext(ctx, "replacing existing subscription failed", "subject", subject, "err", err)
		}
		delete(r.subjects, subject)
	}

	sub, err := r.conn.Subscribe(subject, func(msg *nats.Msg) {
		if err := handler(ctx, Header(msg.Header), msg.Data); err != nil {
			r.log.ErrorContext(ctx, "Subscribe message failed", "subject", subject, "err", err)
		}
	})
	if err != nil {
		r.log.ErrorContext(ctx, "Subscribe failed", "subject", subject, "err", err)
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	r.subjects[subject] = sub
	return nil
}

func (r *natsSubscriber) Unsubscribe(ctx context.Context, subject string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if subscribe, ok := r.subjects[subject]; ok {
		if err := subscribe.Unsubscribe(); err != nil {
			r.log.ErrorContext(ctx, "Unsubscribe failed", "subject", subject, "err", err)
			return
		}
		delete(r.subjects, subject)
	}
}

func (r *natsSubscriber) HasReady() bool {
	return r.conn.Ready()
}
