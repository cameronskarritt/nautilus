package webpush

import (
	"net/http"

	"nautilus/internal/database/pushsubscriptions"
	"nautilus/internal/database/users"
	"nautilus/internal/httputil"
)

func (m *WebPushMux) GetVAPIDKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	httputil.JSON(ctx, w, httputil.Map{
		"public_key": m.vapidPublicKey,
	})
}

func (m *WebPushMux) Subscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := users.FromContext(ctx)

	var form SubscribeForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	sub, err := pushsubscriptions.Create(ctx, m.db, user.ID, form.Endpoint, form.Keys.Auth, form.Keys.P256dh)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	httputil.JSON(ctx, w, httputil.Map{
		"subscription": sub,
	}, http.StatusCreated)
}

func (m *WebPushMux) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := users.FromContext(ctx)

	var form UnsubscribeForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	err := pushsubscriptions.Delete(ctx, m.db, user.ID, form.Endpoint)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
