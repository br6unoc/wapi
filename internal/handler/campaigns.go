package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"botwapp/internal/instance"
	"botwapp/internal/service"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

// CampaignFilters holds optional filter criteria applied on top of audience selection.
type CampaignFilters struct {
	DateFrom     string   `json:"date_from"`     // lead entry from (YYYY-MM-DD)
	DateTo       string   `json:"date_to"`       // lead entry to (YYYY-MM-DD)
	TagIDs       []string `json:"tag_ids"`       // must have all these tags
	InactiveDays int      `json:"inactive_days"` // last_contact_at older than N days (0 = no filter)
	MinPurchases int      `json:"min_purchases"` // -1 = no filter
	MaxPurchases int      `json:"max_purchases"` // -1 = no filter
	ManualPhones string   `json:"manual_phones"` // used when audience_type = 'manual'
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
		Message      string          `json:"message" binding:"required"`
		AudienceType string          `json:"audience_type" binding:"required"`
		AudienceRef  string          `json:"audience_ref"`
		Filters      CampaignFilters `json:"filters"`
		ScheduleType string          `json:"schedule_type"`
		ScheduledAt  string          `json:"scheduled_at"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	var campaignID string
	err = postgres.DB.QueryRow(`
		INSERT INTO campaigns
		  (company_id, created_by, name, message, instance_id, audience_type, audience_ref,
		   filters, schedule_type, scheduled_at, status, total_contacts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id
	`, companyID, userID, body.Name, body.Message, body.InstanceID,
		body.AudienceType, audienceRef, string(filtersJSON), body.ScheduleType, scheduledAt,
		status, len(contacts)).Scan(&campaignID)
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
		go runCampaign(campaignID, body.InstanceID, body.Message)
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

func runCampaign(campaignID, instanceID, messageTemplate string) {
	inst, ok := instance.Global.Get(instanceID)
	if !ok || inst.WAClient == nil || !inst.WAClient.IsConnected() {
		postgres.DB.Exec(`UPDATE campaigns SET status='finished' WHERE id=$1`, campaignID)
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

	for _, p := range list {
		msg := personalizeMessage(messageTemplate, p.Name)
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

		time.Sleep(2 * time.Second)
	}

	postgres.DB.Exec(`UPDATE campaigns SET status='finished', finished_at=NOW() WHERE id=$1`, campaignID)
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
