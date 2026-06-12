package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
	"unicode"

	"botwapp/internal/instance"
	"botwapp/internal/service"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// CampaignFilters holds optional filter criteria applied on top of audience selection.
type CampaignFilters struct {
	DateFrom       string            `json:"date_from"`       // lead entry from (YYYY-MM-DD)
	DateTo         string            `json:"date_to"`         // lead entry to (YYYY-MM-DD)
	TagIDs         []string          `json:"tag_ids"`         // must have all these tags
	InactiveDays   int               `json:"inactive_days"`   // last_contact_at older than N days (0 = no filter)
	MinPurchases   int               `json:"min_purchases"`   // -1 = no filter
	MaxPurchases   int               `json:"max_purchases"`   // -1 = no filter
	ManualPhones   string            `json:"manual_phones"`   // plain phone list (fallback)
	ManualContacts []ManualContact   `json:"manual_contacts"` // from file upload: [{phone, name}]
}

type ManualContact struct {
	Phone string `json:"phone"`
	Name  string `json:"name"`
}

type CampaignView struct {
	ID            string
	Name          string
	Status        string
	InstanceName  string
	AudienceType  string
	TotalContacts int
	SentCount     int
	FailedCount   int
	ScheduledAt   string
	CreatedAt     string
}

type SectorOption struct{ ID, Name string }
type UserOption struct{ ID, Username string }

func WebCampaigns(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	companyID := currentCompanyID(c)
	userID := currentUserID(c)
	role := currentRole(c)

	// Connected instances
	instRows, _ := postgres.DB.Query(
		`SELECT id, name FROM instances WHERE company_id = $1 AND status = 'connected' ORDER BY name`,
		companyID,
	)
	type InstOpt struct{ ID, Name string }
	var instances []InstOpt
	if instRows != nil {
		defer instRows.Close()
		for instRows.Next() {
			var i InstOpt
			instRows.Scan(&i.ID, &i.Name)
			instances = append(instances, i)
		}
	}

	var sectors []SectorOption
	var users []UserOption
	if role == "admin" || role == "super_admin" || role == "coordinator" {
		sRows, _ := postgres.DB.Query(`SELECT id, name FROM sectors WHERE company_id = $1 ORDER BY name`, companyID)
		if sRows != nil {
			defer sRows.Close()
			for sRows.Next() {
				var s SectorOption
				sRows.Scan(&s.ID, &s.Name)
				sectors = append(sectors, s)
			}
		}
		uRows, _ := postgres.DB.Query(`SELECT id, username FROM users WHERE company_id = $1 ORDER BY username`, companyID)
		if uRows != nil {
			defer uRows.Close()
			for uRows.Next() {
				var u UserOption
				uRows.Scan(&u.ID, &u.Username)
				users = append(users, u)
			}
		}
	}

	campaigns, _ := listCampaignsForCompany(companyID)

	render(c, http.StatusOK, "campaigns.html", gin.H{
		"Token":     token,
		"Username":  username,
		"Role":      role,
		"UserID":    userID,
		"Instances": instances,
		"Sectors":   sectors,
		"Users":     users,
		"Campaigns": campaigns,
	})
}

func listCampaignsForCompany(companyID string) ([]CampaignView, error) {
	rows, err := postgres.DB.Query(`
		SELECT ca.id, ca.name, ca.status, i.name,
		       ca.audience_type, ca.total_contacts, ca.sent_count, ca.failed_count,
		       COALESCE(TO_CHAR(ca.scheduled_at, 'DD/MM/YYYY HH24:MI'), ''),
		       TO_CHAR(ca.created_at, 'DD/MM/YYYY HH24:MI')
		FROM campaigns ca
		JOIN instances i ON i.id = ca.instance_id
		WHERE ca.company_id = $1
		ORDER BY ca.created_at DESC
		LIMIT 200
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CampaignView
	for rows.Next() {
		var cv CampaignView
		rows.Scan(&cv.ID, &cv.Name, &cv.Status, &cv.InstanceName,
			&cv.AudienceType, &cv.TotalContacts, &cv.SentCount, &cv.FailedCount,
			&cv.ScheduledAt, &cv.CreatedAt)
		list = append(list, cv)
	}
	return list, nil
}

func APIListCampaigns(c *gin.Context) {
	companyID := currentCompanyID(c)
	list, err := listCampaignsForCompany(companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

func APICreateCampaign(c *gin.Context) {
	companyID := currentCompanyID(c)
	userID := currentUserID(c)
	role := currentRole(c)

	var body struct {
		Name         string          `json:"name" binding:"required"`
		InstanceID   string          `json:"instance_id" binding:"required"`
		Message      string          `json:"message"`  // kept for back-compat
		Messages     []string        `json:"messages"` // up to 6 variants
		AudienceType string          `json:"audience_type" binding:"required"`
		AudienceRef  string          `json:"audience_ref"`
		Filters      CampaignFilters `json:"filters"`
		ScheduleType string          `json:"schedule_type"`
		ScheduledAt  string          `json:"scheduled_at"`
		DripDelay    string          `json:"drip_delay"` // "30s-1m","1m-3m","3m-5m","5m-10m"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Normaliza variantes: filtra vazios, limita a 6
	var variants []string
	for _, m := range body.Messages {
		if strings.TrimSpace(m) != "" {
			variants = append(variants, strings.TrimSpace(m))
		}
		if len(variants) == 6 {
			break
		}
	}
	// Fallback para campo legado
	if len(variants) == 0 && strings.TrimSpace(body.Message) != "" {
		variants = []string{strings.TrimSpace(body.Message)}
	}
	if len(variants) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pelo menos uma mensagem é obrigatória"})
		return
	}
	primaryMessage := variants[0]

	// Non-admin forced to mine
	if role != "admin" && role != "super_admin" && role != "coordinator" {
		if body.AudienceType != "manual" {
			body.AudienceType = "mine"
			body.AudienceRef = ""
		}
	}

	contacts, err := resolveCampaignContacts(companyID, userID, body.AudienceType, body.AudienceRef, body.Filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao resolver contatos: " + err.Error()})
		return
	}
	if len(contacts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nenhum contato encontrado para os filtros selecionados"})
		return
	}

	status := "running"
	var scheduledAt interface{} = nil
	if body.ScheduleType == "scheduled" && body.ScheduledAt != "" {
		t, err := time.Parse("2006-01-02T15:04", body.ScheduledAt)
		if err == nil {
			scheduledAt = t
			status = "scheduled"
		}
	}

	var audienceRef interface{} = nil
	if body.AudienceRef != "" {
		audienceRef = body.AudienceRef
	}

	filtersJSON, _ := json.Marshal(body.Filters)
	variantsJSON, _ := json.Marshal(variants)
	dripMin, dripMax := parseDripDelay(body.DripDelay)

	var campaignID string
	err = postgres.DB.QueryRow(`
		INSERT INTO campaigns
		  (company_id, created_by, name, message, message_variants, instance_id, audience_type, audience_ref,
		   filters, schedule_type, scheduled_at, status, total_contacts, drip_min_seconds, drip_max_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id
	`, companyID, userID, body.Name, primaryMessage, string(variantsJSON), body.InstanceID,
		body.AudienceType, audienceRef, string(filtersJSON), body.ScheduleType, scheduledAt,
		status, len(contacts), dripMin, dripMax).Scan(&campaignID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, ct := range contacts {
		var cid interface{} = nil
		if ct.ID != "" {
			cid = ct.ID
		}
		postgres.DB.Exec(`
			INSERT INTO campaign_contacts (campaign_id, contact_id, phone, name)
			VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING
		`, campaignID, cid, ct.Phone, ct.Name)
	}

	if status == "running" {
		go runCampaign(campaignID, body.InstanceID, variants, dripMin, dripMax)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "id": campaignID, "total": len(contacts)})
}

func APIDeleteCampaign(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)
	res, err := postgres.DB.Exec(
		`DELETE FROM campaigns WHERE id = $1 AND company_id = $2 AND status IN ('scheduled','draft')`,
		id, companyID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Campanha não pode ser excluída (já iniciada ou não encontrada)"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type campaignContact struct {
	ID    string // empty for manual phones
	Phone string
	Name  string
}

func resolveCampaignContacts(companyID, userID, audienceType, audienceRef string, filters CampaignFilters) ([]campaignContact, error) {
	// Manual list — bypass DB contacts
	if audienceType == "manual" {
		var contacts []campaignContact
		// Prefer structured contacts from file upload
		if len(filters.ManualContacts) > 0 {
			for _, mc := range filters.ManualContacts {
				phone := strings.TrimSpace(mc.Phone)
				if phone != "" {
					contacts = append(contacts, campaignContact{Phone: phone, Name: mc.Name})
				}
			}
			return contacts, nil
		}
		// Fallback: plain phone list
		for _, line := range strings.Split(filters.ManualPhones, "\n") {
			phone := strings.TrimSpace(line)
			if phone != "" {
				contacts = append(contacts, campaignContact{Phone: phone})
			}
		}
		return contacts, nil
	}

	// Build base query for company contacts
	var conditions []string
	var args []interface{}
	argN := 1

	conditions = append(conditions, fmt.Sprintf("i.company_id = $%d", argN))
	args = append(args, companyID)
	argN++

	conditions = append(conditions, "ct.is_blocked = FALSE")

	switch audienceType {
	case "sector":
		conditions = append(conditions, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM user_sectors us WHERE us.user_id = ct.assigned_user_id AND us.sector_id = $%d)", argN,
		))
		args = append(args, audienceRef)
		argN++
	case "user":
		conditions = append(conditions, fmt.Sprintf("ct.assigned_user_id = $%d", argN))
		args = append(args, audienceRef)
		argN++
	case "mine":
		conditions = append(conditions, fmt.Sprintf("ct.assigned_user_id = $%d", argN))
		args = append(args, userID)
		argN++
	// 'all' — no extra condition
	}

	// Date range filter on first contact (created_at)
	if filters.DateFrom != "" {
		conditions = append(conditions, fmt.Sprintf("ct.created_at >= $%d", argN))
		args = append(args, filters.DateFrom)
		argN++
	}
	if filters.DateTo != "" {
		conditions = append(conditions, fmt.Sprintf("ct.created_at < ($%d::date + interval '1 day')", argN))
		args = append(args, filters.DateTo)
		argN++
	}

	// Inactive days filter
	if filters.InactiveDays > 0 {
		conditions = append(conditions, fmt.Sprintf(
			"(ct.last_contact_at IS NULL OR ct.last_contact_at < NOW() - ($%d * interval '1 day'))", argN,
		))
		args = append(args, filters.InactiveDays)
		argN++
	}

	// Purchase count filters
	if filters.MinPurchases >= 0 {
		conditions = append(conditions, fmt.Sprintf("ct.purchase_count >= $%d", argN))
		args = append(args, filters.MinPurchases)
		argN++
	}
	if filters.MaxPurchases >= 0 {
		conditions = append(conditions, fmt.Sprintf("ct.purchase_count <= $%d", argN))
		args = append(args, filters.MaxPurchases)
		argN++
	}

	// Tag filter
	if len(filters.TagIDs) > 0 {
		for _, tagID := range filters.TagIDs {
			conditions = append(conditions, fmt.Sprintf(
				"EXISTS (SELECT 1 FROM contact_tag_links ctl WHERE ctl.contact_id = ct.id AND ctl.tag_id = $%d)", argN,
			))
			args = append(args, tagID)
			argN++
		}
	}

	q := fmt.Sprintf(`
		SELECT ct.id, ct.phone, ct.name
		FROM contacts ct
		JOIN instances i ON i.id = ct.instance_id
		WHERE %s
		ORDER BY ct.last_contact_at DESC NULLS LAST
	`, strings.Join(conditions, " AND "))

	rows, err := postgres.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []campaignContact
	for rows.Next() {
		var ct campaignContact
		rows.Scan(&ct.ID, &ct.Phone, &ct.Name)
		contacts = append(contacts, ct)
	}
	return contacts, nil
}

func runCampaign(campaignID, instanceID string, messageVariants []string, dripMin, dripMax int) {
	defer postgres.DB.Exec(`UPDATE campaigns SET status='finished', finished_at=NOW() WHERE id=$1 AND status='running'`, campaignID)

	inst, ok := instance.Global.Get(instanceID)
	if !ok || inst.WAClient == nil || !inst.WAClient.IsConnected() {
		return
	}

	rows, err := postgres.DB.Query(
		`SELECT id, phone, name FROM campaign_contacts WHERE campaign_id = $1 AND status = 'pending'`,
		campaignID,
	)
	if err != nil {
		log.Printf("[campaign] erro ao buscar contatos: %v", err)
		return
	}

	type pending struct{ ID, Phone, Name string }
	var list []pending
	for rows.Next() {
		var p pending
		rows.Scan(&p.ID, &p.Phone, &p.Name)
		list = append(list, p)
	}
	rows.Close()

	postgres.DB.Exec(`UPDATE campaigns SET started_at=NOW() WHERE id=$1`, campaignID)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rangeSeconds := dripMax - dripMin
	if rangeSeconds <= 0 {
		rangeSeconds = 1
	}
	nVariants := len(messageVariants)

	for i, p := range list {
		template := messageVariants[i%nVariants] // rotação circular
		msg := personalizeMessage(template, p.Name)
		err := sendTextViaInstance(inst, p.Phone, msg)

		if err != nil {
			postgres.DB.Exec(
				`UPDATE campaign_contacts SET status='failed', error_msg=$1, sent_at=NOW() WHERE id=$2`,
				err.Error(), p.ID,
			)
			postgres.DB.Exec(`UPDATE campaigns SET failed_count = failed_count + 1 WHERE id=$1`, campaignID)
		} else {
			postgres.DB.Exec(
				`UPDATE campaign_contacts SET status='sent', sent_at=NOW() WHERE id=$1`, p.ID,
			)
			postgres.DB.Exec(`UPDATE campaigns SET sent_count = sent_count + 1 WHERE id=$1`, campaignID)
		}

		// Drip delay: skip after last contact
		if i < len(list)-1 {
			delaySec := dripMin + rng.Intn(rangeSeconds)
			log.Printf("[campaign] aguardando %ds antes do próximo disparo", delaySec)
			time.Sleep(time.Duration(delaySec) * time.Second)
		}
	}

	postgres.DB.Exec(`UPDATE campaigns SET status='finished', finished_at=NOW() WHERE id=$1`, campaignID)
}

// APIParseContactsFile parseia CSV ou XLSX e retorna [{phone, name}].
// Detecta automaticamente quais colunas são telefone e nome.
func APIParseContactsFile(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "arquivo não enviado"})
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao abrir arquivo"})
		return
	}
	defer f.Close()

	name := strings.ToLower(fh.Filename)
	var rows [][]string

	if strings.HasSuffix(name, ".csv") {
		r := csv.NewReader(f)
		r.LazyQuotes = true
		r.TrimLeadingSpace = true
		rows, err = r.ReadAll()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "erro ao ler CSV: " + err.Error()})
			return
		}
	} else if strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".xls") {
		xl, err := excelize.OpenReader(f)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "erro ao ler Excel: " + err.Error()})
			return
		}
		sheets := xl.GetSheetList()
		if len(sheets) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "planilha vazia"})
			return
		}
		rows, err = xl.GetRows(sheets[0])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "erro ao ler planilha"})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "formato não suportado — use .csv ou .xlsx"})
		return
	}

	contacts, warning := parseContactRows(rows)
	c.JSON(http.StatusOK, gin.H{"contacts": contacts, "total": len(contacts), "warning": warning})
}

// parseContactRows detecta colunas de telefone e nome e extrai os contatos.
func parseContactRows(rows [][]string) ([]ManualContact, string) {
	if len(rows) == 0 {
		return nil, ""
	}

	phoneCol, nameCol := -1, -1
	startRow := 0

	// Tenta detectar pelo cabeçalho
	header := rows[0]
	for i, h := range header {
		norm := normalizeHeader(h)
		if phoneCol < 0 && isPhoneHeader(norm) {
			phoneCol = i
		} else if nameCol < 0 && isNameHeader(norm) {
			nameCol = i
		}
	}

	if phoneCol >= 0 {
		startRow = 1 // linha 0 é cabeçalho
	} else {
		// Sem cabeçalho reconhecível — detecta por conteúdo
		for i, cell := range header {
			if looksLikePhone(cell) {
				phoneCol = i
				break
			}
		}
		for i, cell := range header {
			if i != phoneCol && !looksLikePhone(cell) && strings.TrimSpace(cell) != "" {
				nameCol = i
				break
			}
		}
		startRow = 0
	}

	if phoneCol < 0 {
		// Última tentativa: assume coluna 0 = telefone
		phoneCol = 0
		if len(header) > 1 {
			nameCol = 1
		}
		startRow = 0
	}

	warning := ""
	if startRow == 0 && len(rows) > 0 && !looksLikePhone(rows[0][phoneCol]) {
		warning = "Cabeçalho não detectado — verifique se os dados estão corretos"
	}

	var contacts []ManualContact
	seen := map[string]bool{}
	for _, row := range rows[startRow:] {
		if phoneCol >= len(row) {
			continue
		}
		phone := sanitizePhone(row[phoneCol])
		if phone == "" || seen[phone] {
			continue
		}
		seen[phone] = true
		name := ""
		if nameCol >= 0 && nameCol < len(row) {
			name = strings.TrimSpace(row[nameCol])
		}
		contacts = append(contacts, ManualContact{Phone: phone, Name: name})
	}
	return contacts, warning
}

func normalizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPhoneHeader(norm string) bool {
	keywords := []string{"telefone", "fone", "phone", "celular", "cel", "numero", "whatsapp", "wpp", "contato", "tel"}
	for _, k := range keywords {
		if strings.Contains(norm, k) {
			return true
		}
	}
	return false
}

func isNameHeader(norm string) bool {
	keywords := []string{"nome", "name", "cliente", "razao", "empresa", "pessoa"}
	for _, k := range keywords {
		if strings.Contains(norm, k) {
			return true
		}
	}
	return false
}

func looksLikePhone(s string) bool {
	digits := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	return digits >= 8 && digits <= 15
}

func sanitizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseDripDelay converte o preset de faixa em min/max segundos.
// Padrão (sem preset): 30–60s.
func parseDripDelay(preset string) (min, max int) {
	switch preset {
	case "1m-3m":
		return 60, 180
	case "3m-5m":
		return 180, 300
	case "5m-10m":
		return 300, 600
	default: // "30s-1m" ou vazio
		return 30, 60
	}
}

func personalizeMessage(tmpl, name string) string {
	if name == "" {
		name = "cliente"
	}
	return strings.ReplaceAll(tmpl, "{{nome}}", name)
}

func sendTextViaInstance(inst *instance.Instance, phone, text string) error {
	return service.SendText(inst, phone, text)
}
