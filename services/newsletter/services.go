package newsletter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

type MailMessage struct {
	To       string
	From     string
	ReplyTo  string
	Subject  string
	HTML     string
	Headers  map[string]string
	TestSend bool
}

type Mailer interface {
	SendNewsletter(context.Context, MailMessage) error
}

type BounceSuppressionStore struct {
	store *SQLStore
}

func NewBounceSuppressionStore(store *SQLStore) *BounceSuppressionStore {
	return &BounceSuppressionStore{store: store}
}

const suppressionKey = "newsletter.suppressed_emails"

func (b *BounceSuppressionStore) MarkBounced(ctx context.Context, email string) error {
	return b.mark(ctx, email)
}

func (b *BounceSuppressionStore) MarkComplained(ctx context.Context, email string) error {
	return b.mark(ctx, email)
}

func (b *BounceSuppressionStore) IsBounced(ctx context.Context, email string) (bool, error) {
	list, err := b.load(ctx)
	if err != nil {
		return false, err
	}
	_, ok := list[strings.ToLower(email)]
	return ok, nil
}

func (b *BounceSuppressionStore) FilterSendable(ctx context.Context, emails []string) ([]string, error) {
	list, err := b.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, email := range emails {
		if !list[strings.ToLower(email)] {
			out = append(out, email)
		}
	}
	return out, nil
}

func (b *BounceSuppressionStore) mark(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	list, err := b.load(ctx)
	if err != nil {
		return err
	}
	if list[email] {
		return nil
	}
	list[email] = true
	values := make([]string, 0, len(list))
	for e := range list {
		values = append(values, e)
	}
	raw, _ := json.Marshal(values)
	return b.store.SetSetting(ctx, suppressionKey, string(raw))
}

func (b *BounceSuppressionStore) load(ctx context.Context) (map[string]bool, error) {
	raw, err := b.store.GetSetting(ctx, suppressionKey)
	if err != nil {
		return nil, err
	}
	var decoded []string
	_ = json.Unmarshal([]byte(raw), &decoded)
	out := make(map[string]bool, len(decoded))
	for _, email := range decoded {
		email = strings.ToLower(strings.TrimSpace(email))
		if email != "" {
			out[email] = true
		}
	}
	return out, nil
}

type ContactSegmentResolver struct {
	store *SQLStore
}

func NewContactSegmentResolver(store *SQLStore) *ContactSegmentResolver {
	return &ContactSegmentResolver{store: store}
}

var allowedSegmentFields = map[string]string{
	"id":         "id",
	"email":      "email",
	"name":       "name",
	"user_id":    "user_id",
	"created_at": "created_at",
	"updated_at": "updated_at",
}

var allowedSegmentOps = map[string]string{
	"=": "=", "!=": "!=", "<>": "!=", ">": ">", ">=": ">=", "<": "<", "<=": "<=",
	"like": "LIKE", "not_like": "NOT LIKE",
}

func (r *ContactSegmentResolver) Resolve(ctx context.Context, list *models.NewsletterList) ([]int64, error) {
	if list.Kind == models.NewsletterListStatic {
		return r.store.ListMemberIDs(ctx, list.ID)
	}
	return r.filteredIDs(ctx, list.FilterJSON, false)
}

func (r *ContactSegmentResolver) ResolveSendable(ctx context.Context, list *models.NewsletterList) ([]int64, error) {
	if list.Kind == models.NewsletterListStatic {
		ids, err := r.store.ListMemberIDs(ctx, list.ID)
		if err != nil || len(ids) == 0 {
			return ids, err
		}
		contacts, err := r.store.ContactsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		var sendable []int64
		for _, c := range contacts {
			if c.MarketingOptOutAt == nil {
				sendable = append(sendable, c.ID)
			}
		}
		return sendable, nil
	}
	return r.filteredIDs(ctx, list.FilterJSON, true)
}

func (r *ContactSegmentResolver) CountMatches(ctx context.Context, filter map[string]any) (int, error) {
	where, args := r.where(filter, false)
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s`, r.store.t("contacts"), where)
	var n int
	err := r.store.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

func (r *ContactSegmentResolver) filteredIDs(ctx context.Context, filter map[string]any, sendableOnly bool) ([]int64, error) {
	where, args := r.where(filter, sendableOnly)
	q := fmt.Sprintf(`SELECT id FROM %s WHERE %s ORDER BY id`, r.store.t("contacts"), where)
	rows, err := r.store.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *ContactSegmentResolver) where(filter map[string]any, sendableOnly bool) (string, []any) {
	clauses := []string{"1=1"}
	var args []any
	if sendableOnly {
		clauses = append(clauses, "marketing_opt_out_at IS NULL")
	}
	rules, _ := filter["rules"].([]any)
	for _, raw := range rules {
		rule, _ := raw.(map[string]any)
		field, _ := rule["field"].(string)
		opRaw, _ := rule["op"].(string)
		if opRaw == "" {
			opRaw = "="
		}
		op, ok := allowedSegmentOps[strings.ToLower(opRaw)]
		if !ok || field == "" {
			continue
		}
		args = append(args, rule["value"])
		param := r.store.p(len(args))
		if strings.HasPrefix(field, "metadata.") {
			key := strings.TrimPrefix(field, "metadata.")
			needle, _ := json.Marshal(rule["value"])
			args[len(args)-1] = fmt.Sprintf("%%\"%s\":%s%%", strings.ReplaceAll(key, `"`, ""), string(needle))
			clauses = append(clauses, fmt.Sprintf("metadata LIKE %s", param))
			continue
		}
		col, ok := allowedSegmentFields[field]
		if !ok {
			args = args[:len(args)-1]
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s %s %s", col, op, param))
	}
	return strings.Join(clauses, " AND "), args
}

type NewsletterPlanner struct {
	store    *SQLStore
	segments *ContactSegmentResolver
	bounces  *BounceSuppressionStore
}

func NewNewsletterPlanner(store *SQLStore, segments *ContactSegmentResolver, bounces *BounceSuppressionStore) *NewsletterPlanner {
	return &NewsletterPlanner{store: store, segments: segments, bounces: bounces}
}

func (p *NewsletterPlanner) Plan(ctx context.Context, newsletter *models.Newsletter) error {
	now := time.Now()
	if err := p.store.SetNewsletterStatus(ctx, newsletter.ID, models.NewsletterSending, nil); err != nil {
		return err
	}
	list, err := p.store.GetList(ctx, newsletter.TargetListID)
	if err != nil || list == nil {
		return err
	}
	ids, err := p.segments.ResolveSendable(ctx, list)
	if err != nil {
		return err
	}
	contacts, err := p.store.ContactsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	emails := make([]string, 0, len(contacts))
	for _, c := range contacts {
		emails = append(emails, c.Email)
	}
	sendable, err := p.bounces.FilterSendable(ctx, emails)
	if err != nil {
		return err
	}
	sendableSet := make(map[string]bool, len(sendable))
	for _, e := range sendable {
		sendableSet[strings.ToLower(e)] = true
	}
	total := 0
	for _, c := range contacts {
		if !sendableSet[strings.ToLower(c.Email)] {
			continue
		}
		token, err := randomToken()
		if err != nil {
			return err
		}
		d := &models.NewsletterDelivery{
			NewsletterID:  newsletter.ID,
			ContactID:     c.ID,
			EmailAtSend:   c.Email,
			Status:        models.DeliveryPending,
			TrackingToken: token,
			CreatedAt:     now,
		}
		if err := p.store.InsertDelivery(ctx, d); err != nil {
			return err
		}
		total++
	}
	return p.store.SetNewsletterSummaryTotal(ctx, newsletter.ID, total)
}

type DispatcherConfig struct {
	EnableNewsletters   bool
	BatchSize           int
	RateLimitPerMinute  int
	ClaimTimeoutMinutes int
	AutoPauseBounceRate float64
	AutoPauseThreshold  int
	BaseURL             string
}

type NewsletterDispatcher struct {
	store    *SQLStore
	renderer *Renderer
	mailer   Mailer
	cfg      DispatcherConfig
	mu       sync.Mutex
	minute   string
	sent     int
}

func NewNewsletterDispatcher(store *SQLStore, renderer *Renderer, mailer Mailer, cfg DispatcherConfig) *NewsletterDispatcher {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.RateLimitPerMinute <= 0 {
		cfg.RateLimitPerMinute = 60
	}
	if cfg.ClaimTimeoutMinutes <= 0 {
		cfg.ClaimTimeoutMinutes = 10
	}
	if cfg.AutoPauseBounceRate <= 0 {
		cfg.AutoPauseBounceRate = 0.05
	}
	if cfg.AutoPauseThreshold <= 0 {
		cfg.AutoPauseThreshold = 100
	}
	return &NewsletterDispatcher{store: store, renderer: renderer, mailer: mailer, cfg: cfg}
}

func (d *NewsletterDispatcher) DispatchBatch(ctx context.Context) error {
	if !d.cfg.EnableNewsletters {
		return nil
	}
	now := time.Now()
	if err := d.reclaimStuckRows(ctx, now); err != nil {
		return err
	}
	allowance := d.allowance(now)
	if allowance > 0 {
		limit := min(d.cfg.BatchSize, allowance)
		ids, err := d.claim(ctx, limit, now)
		if err != nil {
			return err
		}
		d.increment(now, len(ids))
		for _, id := range ids {
			if err := d.dispatchOne(ctx, id); err != nil {
				return err
			}
		}
	}
	if err := d.finalizeCompleted(ctx); err != nil {
		return err
	}
	return d.autoPause(ctx)
}

func (d *NewsletterDispatcher) allowance(now time.Time) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := now.UTC().Format("200601021504")
	if d.minute != key {
		d.minute = key
		d.sent = 0
	}
	return max(0, d.cfg.RateLimitPerMinute-d.sent)
}

func (d *NewsletterDispatcher) increment(now time.Time, count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := now.UTC().Format("200601021504")
	if d.minute != key {
		d.minute = key
		d.sent = 0
	}
	d.sent += count
}

func (d *NewsletterDispatcher) claim(ctx context.Context, limit int, now time.Time) ([]int64, error) {
	q := fmt.Sprintf(`SELECT id FROM %s WHERE status=%s AND (next_attempt_at IS NULL OR next_attempt_at <= %s) ORDER BY id LIMIT %d`,
		d.store.t("newsletter_deliveries"), d.store.p(1), d.store.p(2), limit)
	rows, err := d.store.db.QueryContext(ctx, q, models.DeliveryPending, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return ids, err
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for _, id := range ids {
		args = append(args, id)
	}
	u := fmt.Sprintf(`UPDATE %s SET status='%s', claimed_at=%s WHERE id IN (%s) AND status='%s'`,
		d.store.t("newsletter_deliveries"), models.DeliveryQueued, d.store.p(1), d.store.placeholders(2, len(ids)), models.DeliveryPending)
	_, err = d.store.db.ExecContext(ctx, u, args...)
	return ids, err
}

func (d *NewsletterDispatcher) dispatchOne(ctx context.Context, id int64) error {
	delivery, err := d.store.GetDelivery(ctx, id)
	if err != nil || delivery == nil {
		return err
	}
	newsletter, err := d.store.GetNewsletter(ctx, delivery.NewsletterID)
	if err != nil || newsletter == nil {
		return err
	}
	contact, err := d.store.GetContact(ctx, delivery.ContactID)
	if err != nil {
		return err
	}
	var tpl *models.NewsletterTemplate
	if newsletter.TemplateID != nil {
		tpl, _ = d.store.GetTemplate(ctx, *newsletter.TemplateID)
	}
	html, err := d.renderer.Render(delivery, newsletter, contact, tpl)
	if err == nil && d.mailer == nil {
		err = fmt.Errorf("mailer not configured")
	}
	if err == nil {
		err = d.mailer.SendNewsletter(ctx, MailMessage{
			To:      delivery.EmailAtSend,
			From:    formatFrom(newsletter.FromEmail, newsletter.FromName),
			ReplyTo: strDeref(newsletter.ReplyTo),
			Subject: newsletter.Subject,
			HTML:    html,
			Headers: newsletterHeaders(d.cfg.BaseURL, newsletter.ID, delivery.TrackingToken, d.renderer.UnsubscribeURL(delivery)),
		})
	}
	if err == nil {
		now := time.Now()
		delivery.Status = models.DeliverySent
		delivery.SentAt = &now
		delivery.ClaimedAt = nil
		delivery.NextAttemptAt = nil
		if err := d.store.UpdateDelivery(ctx, delivery); err != nil {
			return err
		}
		return d.store.IncrementNewsletter(ctx, delivery.NewsletterID, "summary_sent", 1)
	}
	attempts := delivery.AttemptCount + 1
	delivery.AttemptCount = attempts
	delivery.ClaimedAt = nil
	reason := err.Error()
	delivery.FailureReason = &reason
	if attempts >= 3 {
		delivery.Status = models.DeliveryFailed
		delivery.NextAttemptAt = nil
	} else {
		delivery.Status = models.DeliveryPending
		next := time.Now().Add(time.Duration([]int{1, 5, 30}[attempts-1]) * time.Minute)
		delivery.NextAttemptAt = &next
	}
	return d.store.UpdateDelivery(ctx, delivery)
}

func newsletterHeaders(baseURL string, newsletterID int64, token, unsub string) map[string]string {
	host := "localhost"
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return map[string]string{
		"List-Unsubscribe":          "<" + unsub + ">",
		"List-Unsubscribe-Post":     "List-Unsubscribe=One-Click",
		"X-Escalated-Newsletter-Id": fmt.Sprint(newsletterID),
		"Message-ID":                fmt.Sprintf("<n-%d-%s@%s>", newsletterID, token, host),
	}
}

func formatFrom(email string, name *string) string {
	if name != nil && *name != "" {
		return fmt.Sprintf(`"%s" <%s>`, *name, email)
	}
	return email
}

func (d *NewsletterDispatcher) reclaimStuckRows(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-time.Duration(d.cfg.ClaimTimeoutMinutes) * time.Minute)
	q := fmt.Sprintf(`UPDATE %s SET status='%s', claimed_at=NULL WHERE status='%s' AND claimed_at < %s`,
		d.store.t("newsletter_deliveries"), models.DeliveryPending, models.DeliveryQueued, d.store.p(1))
	_, err := d.store.db.ExecContext(ctx, q, cutoff)
	return err
}

func (d *NewsletterDispatcher) finalizeCompleted(ctx context.Context) error {
	q := fmt.Sprintf(`SELECT id FROM %s WHERE status=%s`, d.store.t("newsletters"), d.store.p(1))
	rows, err := d.store.db.QueryContext(ctx, q, models.NewsletterSending)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		var remaining int
		cq := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE newsletter_id=%s AND status IN ('pending','queued')`, d.store.t("newsletter_deliveries"), d.store.p(1))
		if err := d.store.db.QueryRowContext(ctx, cq, id).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			now := time.Now()
			if err := d.store.SetNewsletterStatus(ctx, id, models.NewsletterSent, &now); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func (d *NewsletterDispatcher) autoPause(ctx context.Context) error {
	q := fmt.Sprintf(`SELECT id FROM %s WHERE status=%s`, d.store.t("newsletters"), d.store.p(1))
	rows, err := d.store.db.QueryContext(ctx, q, models.NewsletterSending)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		tq := fmt.Sprintf(`SELECT status FROM %s WHERE newsletter_id=%s AND status IN ('sent','bounced','complained','failed') ORDER BY id LIMIT %d`,
			d.store.t("newsletter_deliveries"), d.store.p(1), d.cfg.AutoPauseThreshold)
		tr, err := d.store.db.QueryContext(ctx, tq, id)
		if err != nil {
			return err
		}
		var total, bounced int
		for tr.Next() {
			var st string
			if err := tr.Scan(&st); err != nil {
				_ = tr.Close()
				return err
			}
			total++
			if st == string(models.DeliveryBounced) {
				bounced++
			}
		}
		_ = tr.Close()
		if total >= d.cfg.AutoPauseThreshold && float64(bounced)/float64(d.cfg.AutoPauseThreshold) >= d.cfg.AutoPauseBounceRate {
			if err := d.store.SetNewsletterStatus(ctx, id, models.NewsletterPaused, nil); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

type NewsletterTracker struct {
	store   *SQLStore
	bounces *BounceSuppressionStore
}

func NewNewsletterTracker(store *SQLStore, bounces *BounceSuppressionStore) *NewsletterTracker {
	return &NewsletterTracker{store: store, bounces: bounces}
}

func (t *NewsletterTracker) RecordOpen(ctx context.Context, token string) {
	d, err := t.store.GetDeliveryByToken(ctx, token)
	if err != nil || d == nil || terminal(d.Status) || d.OpenedAt != nil {
		return
	}
	now := time.Now()
	d.OpenedAt = &now
	if t.store.UpdateDelivery(ctx, d) == nil {
		_ = t.store.IncrementNewsletter(ctx, d.NewsletterID, "summary_opened", 1)
	}
}

func (t *NewsletterTracker) RecordClick(ctx context.Context, token, _ string) {
	d, err := t.store.GetDeliveryByToken(ctx, token)
	if err != nil || d == nil || terminal(d.Status) {
		return
	}
	first := d.ClicksCount == 0
	now := time.Now()
	d.ClicksCount++
	d.LastClickedAt = &now
	implicitOpen := d.OpenedAt == nil
	if implicitOpen {
		d.OpenedAt = &now
	}
	if t.store.UpdateDelivery(ctx, d) != nil {
		return
	}
	if implicitOpen {
		_ = t.store.IncrementNewsletter(ctx, d.NewsletterID, "summary_opened", 1)
	}
	if first {
		_ = t.store.IncrementNewsletter(ctx, d.NewsletterID, "summary_clicked", 1)
	}
}

func (t *NewsletterTracker) RecordBounce(ctx context.Context, token, typ string, reason *string) {
	if typ != "hard" {
		return
	}
	d, err := t.store.GetDeliveryByToken(ctx, token)
	if err != nil || d == nil || d.Status == models.DeliveryBounced {
		return
	}
	d.Status = models.DeliveryBounced
	d.BounceReason = reason
	if t.store.UpdateDelivery(ctx, d) == nil {
		_ = t.store.IncrementNewsletter(ctx, d.NewsletterID, "summary_bounced", 1)
		_ = t.bounces.MarkBounced(ctx, d.EmailAtSend)
	}
}

func (t *NewsletterTracker) RecordComplaint(ctx context.Context, token string) {
	d, err := t.store.GetDeliveryByToken(ctx, token)
	if err != nil || d == nil || d.Status == models.DeliveryComplained {
		return
	}
	d.Status = models.DeliveryComplained
	if t.store.UpdateDelivery(ctx, d) == nil {
		_ = t.store.IncrementNewsletter(ctx, d.NewsletterID, "summary_complained", 1)
		_ = t.bounces.MarkComplained(ctx, d.EmailAtSend)
	}
}

func terminal(st models.NewsletterDeliveryStatus) bool {
	return st == models.DeliveryBounced || st == models.DeliveryComplained || st == models.DeliveryFailed
}

type Worker struct {
	planner    *NewsletterPlanner
	dispatcher *NewsletterDispatcher
	store      *SQLStore
	enabled    func() bool
	mu         sync.Mutex
}

func NewWorker(store *SQLStore, planner *NewsletterPlanner, dispatcher *NewsletterDispatcher, enabled func() bool) *Worker {
	return &Worker{store: store, planner: planner, dispatcher: dispatcher, enabled: enabled}
}

func (w *Worker) Tick(ctx context.Context) error {
	if w.enabled != nil && !w.enabled() {
		return nil
	}
	if !w.mu.TryLock() {
		return nil
	}
	defer w.mu.Unlock()
	due, err := w.store.ListScheduledDue(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, n := range due {
		if err := w.planner.Plan(ctx, n); err != nil {
			return err
		}
	}
	return w.dispatcher.DispatchBatch(ctx)
}

func (w *Worker) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = w.Tick(ctx)
			}
		}
	}()
	return cancel
}
