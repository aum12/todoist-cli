
package types

import "encoding/json"

type ActionView struct {
	Name string `json:"name"`
}

type ActivityEvents struct {
	EventDate         string `json:"event_date"`
	EventType         string `json:"event_type"`
	ExtraData         string `json:"extra_data"`
	ExtraDataId       string `json:"extra_data_id"`
	Id                string `json:"id"`
	InitiatorId       string `json:"initiator_id"`
	ObjectId          string `json:"object_id"`
	ObjectType        string `json:"object_type"`
	ParentItemId      string `json:"parent_item_id"`
	ParentProjectId   string `json:"parent_project_id"`
	V2ObjectId        string `json:"v2_object_id"`
	V2ParentItemId    string `json:"v2_parent_item_id"`
	V2ParentProjectId string `json:"v2_parent_project_id"`
}

type Body_02786f52 struct {
	ParentId  string `json:"parent_id"`
	ProjectId string `json:"project_id"`
	SectionId string `json:"section_id"`
}

type Body_0680de45 struct {
	Name string `json:"name"`
}

type Body_09503d19 struct {
	DefaultOrder string `json:"default_order"`
	Name         string `json:"name"`
}

type Body_0d5e560b struct {
	Due          string `json:"due"`
	IsUrgent     bool   `json:"is_urgent"`
	MinuteOffset string `json:"minute_offset"`
	Service      string `json:"service"`
}

type Body_0dc72dfd struct {
	ObjId   string `json:"obj_id"`
	ObjType string `json:"obj_type"`
}

type Body_108b7f01 struct {
	EmailList string `json:"email_list"`
	Role      string `json:"role"`
}

type Body_134c19e9 struct {
	Name      string `json:"name"`
	Order     string `json:"order"`
	ProjectId string `json:"project_id"`
}

type Body_163067b4 struct {
	IsCollapsed  string `json:"is_collapsed"`
	Name         string `json:"name"`
	SectionOrder string `json:"section_order"`
}

type Body_207bf8bc struct {
	Color       string `json:"color"`
	Description string `json:"description"`
	IsFavorite  bool   `json:"is_favorite"`
	Name        string `json:"name"`
	ParentId    string `json:"parent_id"`
	ViewStyle   string `json:"view_style"`
	WorkspaceId string `json:"workspace_id"`
}

type Body_28d2b1b0 struct {
	Attachment   string `json:"attachment"`
	Content      string `json:"content"`
	ProjectId    string `json:"project_id"`
	TaskId       string `json:"task_id"`
	UidsToNotify string `json:"uids_to_notify"`
}

type Body_37565102 struct {
	AssigneeId   string `json:"assignee_id"`
	Content      string `json:"content"`
	DeadlineDate string `json:"deadline_date"`
	Description  string `json:"description"`
	DueDate      string `json:"due_date"`
	DueDatetime  string `json:"due_datetime"`
	DueLang      string `json:"due_lang"`
	DueString    string `json:"due_string"`
	Duration     string `json:"duration"`
	DurationUnit string `json:"duration_unit"`
	Labels       string `json:"labels"`
	Order        string `json:"order"`
	ParentId     string `json:"parent_id"`
	Priority     string `json:"priority"`
	ProjectId    string `json:"project_id"`
	SectionId    string `json:"section_id"`
}

type Body_38bb43e2 struct {
	Color      string `json:"color"`
	IsFavorite bool   `json:"is_favorite"`
	Name       string `json:"name"`
	Order      string `json:"order"`
}

type Body_40158d1f struct {
	AutoReminder bool   `json:"auto_reminder"`
	Meta         bool   `json:"meta"`
	Note         string `json:"note"`
	Reminder     string `json:"reminder"`
	Text         string `json:"text"`
}

type Body_4c96330f struct {
	LocLat     string `json:"loc_lat"`
	LocLong    string `json:"loc_long"`
	LocTrigger string `json:"loc_trigger"`
	Name       string `json:"name"`
	Radius     int    `json:"radius"`
	TaskId     string `json:"task_id"`
}

type Body_51341cd2 struct {
	Role string `json:"role"`
}

type Body_52bab197 struct {
	UserEmail   string `json:"user_email"`
	WorkspaceId int    `json:"workspace_id"`
}

type Body_604e3e52 struct {
	DontNotify       string `json:"dont_notify"`
	NotificationType string `json:"notification_type"`
	Service          string `json:"service"`
}

type Body_61d93e0e struct {
	Content string `json:"content"`
}

type Body_6ec83bd4 struct {
	Name    string `json:"name"`
	NewName string `json:"new_name"`
}

type Body_829067ab struct {
	AssigneeId   string          `json:"assignee_id"`
	ChildOrder   int             `json:"child_order"`
	Content      string          `json:"content"`
	DayOrder     int             `json:"day_order"`
	DeadlineDate string          `json:"deadline_date"`
	Description  string          `json:"description"`
	DueDate      string          `json:"due_date"`
	DueDatetime  string          `json:"due_datetime"`
	DueLang      string          `json:"due_lang"`
	DueString    string          `json:"due_string"`
	Duration     string          `json:"duration"`
	DurationUnit string          `json:"duration_unit"`
	IsCollapsed  bool            `json:"is_collapsed"`
	Labels       json.RawMessage `json:"labels"`
	Priority     int             `json:"priority"`
}

type Body_8e7e8a3c struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint"`
}

type Body_925d2d9b struct {
	Locale     string `json:"locale"`
	ProjectId  string `json:"project_id"`
	TemplateId string `json:"template_id"`
}

type Body_958e83bd struct {
	Due          string `json:"due"`
	IsUrgent     bool   `json:"is_urgent"`
	MinuteOffset string `json:"minute_offset"`
	ReminderType string `json:"reminder_type"`
	Service      string `json:"service"`
	TaskId       string `json:"task_id"`
}

type Body_999fd79a struct {
	ClientId      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	PersonalToken string `json:"personal_token"`
	Scope         string `json:"scope"`
}

type Body_9c2f879b struct {
	LocLat     string `json:"loc_lat"`
	LocLong    string `json:"loc_long"`
	LocTrigger string `json:"loc_trigger"`
	Name       string `json:"name"`
	Radius     string `json:"radius"`
}

type Body_9c41452d struct {
	Color      string `json:"color"`
	IsFavorite string `json:"is_favorite"`
	Name       string `json:"name"`
	Order      string `json:"order"`
}

type Body_b628cfc1 struct {
	Description          string `json:"description"`
	DomainDiscovery      string `json:"domain_discovery"`
	DomainName           string `json:"domain_name"`
	InviteCode           string `json:"invite_code"`
	IsCollapsed          string `json:"is_collapsed"`
	IsGuestAllowed       string `json:"is_guest_allowed"`
	IsLinkSharingEnabled string `json:"is_link_sharing_enabled"`
	IsTrialPending       string `json:"is_trial_pending"`
	Name                 string `json:"name"`
	Properties           string `json:"properties"`
	RestrictEmailDomains string `json:"restrict_email_domains"`
}

type Body_c04ca410 struct {
	ChildOrder   string `json:"child_order"`
	DefaultOrder string `json:"default_order"`
	Name         string `json:"name"`
	WorkspaceId  int    `json:"workspace_id"`
}

type Body_cb8d28ec struct {
	ChildOrder  string `json:"child_order"`
	Color       string `json:"color"`
	Description string `json:"description"`
	FolderId    string `json:"folder_id"`
	IsCollapsed string `json:"is_collapsed"`
	IsFavorite  string `json:"is_favorite"`
	Name        string `json:"name"`
	ViewStyle   string `json:"view_style"`
}

type Body_create_project_from_file_api_v1_templates_create_project_from_file_post struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	WorkspaceId string `json:"workspace_id"`
}

type Body_e1a528d7 struct {
	Description          string `json:"description"`
	DomainDiscovery      bool   `json:"domain_discovery"`
	DomainName           string `json:"domain_name"`
	IsGuestAllowed       bool   `json:"is_guest_allowed"`
	IsLinkSharingEnabled bool   `json:"is_link_sharing_enabled"`
	IsTrialPending       bool   `json:"is_trial_pending"`
	Name                 string `json:"name"`
	Properties           string `json:"properties"`
	RestrictEmailDomains bool   `json:"restrict_email_domains"`
}

type Body_e4b32da9 struct {
	ReasonDescription string `json:"reason_description"`
	ReasonFlag        string `json:"reason_flag"`
	ReasonText        string `json:"reason_text"`
	ReturnUrl         string `json:"return_url"`
}

type Body_fe5aa153 struct {
	InviteCode  string `json:"invite_code"`
	WorkspaceId string `json:"workspace_id"`
}

type Body_import_into_project_from_file_api_v1_templates_import_into_project_from_file_post struct {
	File      string `json:"file"`
	ProjectId string `json:"project_id"`
}

type Body_update_logo_api_v1_workspaces_logo_post struct {
	Delete      bool   `json:"delete"`
	File        string `json:"file"`
	WorkspaceId int    `json:"workspace_id"`
}

type Body_upload_file_api_v1_uploads_post struct {
	File      string `json:"file"`
	FileName  string `json:"file_name"`
	ProjectId string `json:"project_id"`
}

type Collaborator struct {
	Email string `json:"email"`
	Id    string `json:"id"`
	Name  string `json:"name"`
}

type DailyCompletionItem struct {
	Date           string          `json:"date"`
	Items          json.RawMessage `json:"items"`
	TotalCompleted int             `json:"total_completed"`
}

type ExposedCollaboratorSyncView struct {
	Email     string `json:"email"`
	FullName  string `json:"full_name"`
	Id        string `json:"id"`
	ImageId   string `json:"image_id"`
	IsDeleted bool   `json:"is_deleted"`
	Timezone  string `json:"timezone"`
}

type FileURLResponse struct {
	FileName string `json:"file_name"`
	FileUrl  string `json:"file_url"`
}

type FolderSyncView struct {
	ChildOrder   int    `json:"child_order"`
	DefaultOrder int    `json:"default_order"`
	Id           string `json:"id"`
	IsDeleted    bool   `json:"is_deleted"`
	Name         string `json:"name"`
	WorkspaceId  string `json:"workspace_id"`
}

type FolderView struct {
	ChildOrder   int    `json:"child_order"`
	DefaultOrder int    `json:"default_order"`
	Id           string `json:"id"`
	IsDeleted    bool   `json:"is_deleted"`
	Name         string `json:"name"`
	WorkspaceId  string `json:"workspace_id"`
}

type FormattedPrice struct {
	Currency    string `json:"currency"`
	TaxBehavior string `json:"tax_behavior"`
	UnitAmount  int    `json:"unit_amount"`
}

type FormattedPriceListing struct {
	BillingCycle string          `json:"billing_cycle"`
	Prices       json.RawMessage `json:"prices"`
}

type GoalsSettings struct {
	CurrentDailyStreak  json.RawMessage `json:"current_daily_streak"`
	CurrentWeeklyStreak json.RawMessage `json:"current_weekly_streak"`
	DailyGoal           int             `json:"daily_goal"`
	IgnoreDays          json.RawMessage `json:"ignore_days"`
	KarmaDisabled       int             `json:"karma_disabled"`
	LastDailyStreak     json.RawMessage `json:"last_daily_streak"`
	LastWeeklyStreak    json.RawMessage `json:"last_weekly_streak"`
	MaxDailyStreak      json.RawMessage `json:"max_daily_streak"`
	MaxWeeklyStreak     json.RawMessage `json:"max_weekly_streak"`
	User                string          `json:"user"`
	UserId              string          `json:"user_id"`
	VacationMode        int             `json:"vacation_mode"`
	WeeklyGoal          int             `json:"weekly_goal"`
}

type GraphPoint struct {
	Date     string `json:"date"`
	KarmaAvg int    `json:"karma_avg"`
}

type HTTPValidationError struct {
	Detail json.RawMessage `json:"detail"`
}

type IDMapping struct {
	NewId string `json:"new_id"`
	OldId string `json:"old_id"`
}

type InviteWorkspaceUsersResponse struct {
	InvitedEmails json.RawMessage `json:"invited_emails"`
}

type ItemNoteSyncView struct {
	Content        string `json:"content"`
	FileAttachment string `json:"file_attachment"`
	Id             string `json:"id"`
	IsDeleted      bool   `json:"is_deleted"`
	ItemId         string `json:"item_id"`
	PostedAt       string `json:"posted_at"`
	PostedUid      string `json:"posted_uid"`
	Reactions      string `json:"reactions"`
	UidsToNotify   string `json:"uids_to_notify"`
}

type ItemSyncView struct {
	AddedAt        string          `json:"added_at"`
	AddedByUid     string          `json:"added_by_uid"`
	AssignedByUid  string          `json:"assigned_by_uid"`
	Checked        bool            `json:"checked"`
	ChildOrder     int             `json:"child_order"`
	CompletedAt    string          `json:"completed_at"`
	CompletedByUid string          `json:"completed_by_uid"`
	Content        string          `json:"content"`
	DayOrder       int             `json:"day_order"`
	Deadline       string          `json:"deadline"`
	Description    string          `json:"description"`
	Due            string          `json:"due"`
	Duration       string          `json:"duration"`
	GoalIds        json.RawMessage `json:"goal_ids"`
	Id             string          `json:"id"`
	IsCollapsed    bool            `json:"is_collapsed"`
	IsDeleted      bool            `json:"is_deleted"`
	Labels         json.RawMessage `json:"labels"`
	NoteCount      int             `json:"note_count"`
	ParentId       string          `json:"parent_id"`
	Priority       int             `json:"priority"`
	ProjectId      string          `json:"project_id"`
	ResponsibleUid string          `json:"responsible_uid"`
	SectionId      string          `json:"section_id"`
	UpdatedAt      string          `json:"updated_at"`
	UserId         string          `json:"user_id"`
}

type KarmaUpdateReasonEntry struct {
	NegativeKarma        float64         `json:"negative_karma"`
	NegativeKarmaReasons json.RawMessage `json:"negative_karma_reasons"`
	NewKarma             float64         `json:"new_karma"`
	PositiveKarma        float64         `json:"positive_karma"`
	PositiveKarmaReasons json.RawMessage `json:"positive_karma_reasons"`
	Time                 string          `json:"time"`
}

type LabelRestView struct {
	Color      string `json:"color"`
	Id         string `json:"id"`
	IsFavorite bool   `json:"is_favorite"`
	Name       string `json:"name"`
	Order      string `json:"order"`
}

type MemberView struct {
	FullName    string `json:"full_name"`
	ImageId     string `json:"image_id"`
	IsDeleted   bool   `json:"is_deleted"`
	Role        string `json:"role"`
	Timezone    string `json:"timezone"`
	UserEmail   string `json:"user_email"`
	UserId      string `json:"user_id"`
	WorkspaceId string `json:"workspace_id"`
}

type NoteSyncView struct {
	Content        string `json:"content"`
	FileAttachment string `json:"file_attachment"`
	Id             string `json:"id"`
	IsDeleted      bool   `json:"is_deleted"`
	PostedAt       string `json:"posted_at"`
	PostedUid      string `json:"posted_uid"`
	Reactions      string `json:"reactions"`
	UidsToNotify   string `json:"uids_to_notify"`
}

type NotificationLocationSyncView struct {
	Id         string `json:"id"`
	IsDeleted  bool   `json:"is_deleted"`
	ItemId     string `json:"item_id"`
	LocLat     string `json:"loc_lat"`
	LocLong    string `json:"loc_long"`
	LocTrigger string `json:"loc_trigger"`
	Name       string `json:"name"`
	NotifyUid  string `json:"notify_uid"`
	ProjectId  string `json:"project_id"`
	Radius     int    `json:"radius"`
	Type       string `json:"type"`
}

type NotificationSyncView struct {
	Due          string `json:"due"`
	Id           string `json:"id"`
	IsDeleted    bool   `json:"is_deleted"`
	IsUrgent     bool   `json:"is_urgent"`
	ItemId       string `json:"item_id"`
	MinuteOffset string `json:"minute_offset"`
	NotifyUid    string `json:"notify_uid"`
	Type         string `json:"type"`
}

type PaginatedList_ActivityEvents_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_Annotated_Union_PersonalProjectSyncView__WorkspaceProjectSyncView___FieldInfo_annotation_NoneType__required_True__title__Project_object______ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_Collaborator_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_FolderSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_ItemSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_LabelRestView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_NoteSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_NotificationLocationSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_NotificationSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_ProjectV1View_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_SectionSyncView_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PaginatedList_str_ struct {
	NextCursor string          `json:"next_cursor"`
	Results    json.RawMessage `json:"results"`
}

type PersonalProjectSyncView struct {
	Access         string          `json:"access"`
	CanAssignTasks bool            `json:"can_assign_tasks"`
	CanComment     bool            `json:"can_comment"`
	ChildOrder     int             `json:"child_order"`
	Color          string          `json:"color"`
	CreatedAt      string          `json:"created_at"`
	CreatorUid     string          `json:"creator_uid"`
	DefaultOrder   int             `json:"default_order"`
	Description    string          `json:"description"`
	GoalIds        json.RawMessage `json:"goal_ids"`
	Id             string          `json:"id"`
	InboxProject   bool            `json:"inbox_project"`
	IsArchived     bool            `json:"is_archived"`
	IsCollapsed    bool            `json:"is_collapsed"`
	IsDeleted      bool            `json:"is_deleted"`
	IsFavorite     bool            `json:"is_favorite"`
	IsFrozen       bool            `json:"is_frozen"`
	IsShared       bool            `json:"is_shared"`
	Name           string          `json:"name"`
	ParentId       string          `json:"parent_id"`
	PublicKey      string          `json:"public_key"`
	Role           string          `json:"role"`
	UpdatedAt      string          `json:"updated_at"`
	ViewStyle      string          `json:"view_style"`
}

type PlanPrice struct {
	Amount       string  `json:"amount"`
	BillingCycle string  `json:"billing_cycle"`
	Currency     string  `json:"currency"`
	RawAmount    float64 `json:"raw_amount"`
	TaxBehavior  string  `json:"tax_behavior"`
}

type ProductivityStatsResponse struct {
	CompletedCount     int             `json:"completed_count"`
	DaysItems          json.RawMessage `json:"days_items"`
	Goals              json.RawMessage `json:"goals"`
	Karma              float64         `json:"karma"`
	KarmaGraphData     json.RawMessage `json:"karma_graph_data"`
	KarmaLastUpdate    float64         `json:"karma_last_update"`
	KarmaTrend         string          `json:"karma_trend"`
	KarmaUpdateReasons json.RawMessage `json:"karma_update_reasons"`
	ProjectColors      json.RawMessage `json:"project_colors"`
	WeekItems          json.RawMessage `json:"week_items"`
}

type ProjectAccessView struct {
	Configuration string `json:"configuration"`
	Visibility    string `json:"visibility"`
}

type ProjectCompletedItem struct {
	Completed int    `json:"completed"`
	Id        string `json:"id"`
}

type ProjectImportCreateResponseWithObjects struct {
	Comments     json.RawMessage `json:"comments"`
	ProjectId    string          `json:"project_id"`
	ProjectNotes json.RawMessage `json:"project_notes"`
	Projects     json.RawMessage `json:"projects"`
	Sections     json.RawMessage `json:"sections"`
	Status       string          `json:"status"`
	Tasks        json.RawMessage `json:"tasks"`
	TemplateType string          `json:"template_type"`
}

type ProjectImportResponse struct {
	Comments     json.RawMessage `json:"comments"`
	ProjectNotes json.RawMessage `json:"project_notes"`
	Projects     json.RawMessage `json:"projects"`
	Sections     json.RawMessage `json:"sections"`
	Status       string          `json:"status"`
	Tasks        json.RawMessage `json:"tasks"`
	TemplateType string          `json:"template_type"`
}

type ProjectNoteSyncView struct {
	Content        string `json:"content"`
	FileAttachment string `json:"file_attachment"`
	Id             string `json:"id"`
	IsDeleted      bool   `json:"is_deleted"`
	PostedAt       string `json:"posted_at"`
	PostedUid      string `json:"posted_uid"`
	ProjectId      string `json:"project_id"`
	Reactions      string `json:"reactions"`
	UidsToNotify   string `json:"uids_to_notify"`
}

type PublicProjectConfiguration struct {
	DisableDuplication      bool `json:"disable_duplication"`
	HideCollaboratorDetails bool `json:"hide_collaborator_details"`
}

type ReminderDueAttribute struct {
	Date        string `json:"date"`
	IsRecurring string `json:"is_recurring"`
	Lang        string `json:"lang"`
	String      string `json:"string"`
	Timezone    string `json:"timezone"`
}

type RemoveWorkspaceUserResponse struct {
	Status string `json:"status"`
}

type ResponseCreateWorkspaceApiV1WorkspacesPost struct {
}

type ResponseGetWorkspaceApiV1WorkspacesWorkspaceIdGet struct {
}

type ResponseQuickAddApiV1TasksQuickPost struct {
}

type ResponseUpdateNotificationSettingApiV1NotificationSettingPut struct {
}

type ResponseUpdateWorkspaceApiV1WorkspacesWorkspaceIdPost struct {
}

type RestrictedProjectConfiguration struct {
}

type RoleToActionMappingView struct {
	ProjectCollaboratorActions   json.RawMessage `json:"project_collaborator_actions"`
	WorkspaceCollaboratorActions json.RawMessage `json:"workspace_collaborator_actions"`
}

type RoleView struct {
	Actions json.RawMessage `json:"actions"`
	Name    string          `json:"name"`
}

type SectionSyncView struct {
	AddedAt      string          `json:"added_at"`
	ArchivedAt   string          `json:"archived_at"`
	GoalIds      json.RawMessage `json:"goal_ids"`
	Id           string          `json:"id"`
	IsArchived   bool            `json:"is_archived"`
	IsCollapsed  bool            `json:"is_collapsed"`
	IsDeleted    bool            `json:"is_deleted"`
	Name         string          `json:"name"`
	ProjectId    string          `json:"project_id"`
	SectionOrder int             `json:"section_order"`
	UpdatedAt    string          `json:"updated_at"`
	UserId       string          `json:"user_id"`
}

type StreakInfo struct {
	Count int    `json:"count"`
	End   string `json:"end"`
	Start string `json:"start"`
}

type SubscriptionInfo struct {
	ActivationMethod               string `json:"activation_method"`
	BillingPortalSwitchToAnnualUrl string `json:"billing_portal_switch_to_annual_url"`
	BillingPortalUrl               string `json:"billing_portal_url"`
	ExpirationDate                 string `json:"expiration_date"`
	HasBillingPortal               bool   `json:"has_billing_portal"`
	HasBillingPortalSwitchToAnnual bool   `json:"has_billing_portal_switch_to_annual"`
	HasSwitchLegacyToCurrent       bool   `json:"has_switch_legacy_to_current"`
	InvoiceCreditBalance           string `json:"invoice_credit_balance"`
	Plan                           string `json:"plan"`
	PlanPrice                      string `json:"plan_price"`
	Status                         string `json:"status"`
}

type T_BackupResponse struct {
	Url     string `json:"url"`
	Version string `json:"version"`
}

type T_BillingPortalUrlResponse struct {
	BillingPortalUrl string `json:"billing_portal_url"`
}

type T_EmailResponse struct {
	Email string `json:"email"`
}

type T_GetDataV2Response struct {
	CollaboratorStates json.RawMessage `json:"collaborator_states"`
	Collaborators      json.RawMessage `json:"collaborators"`
	Comments           json.RawMessage `json:"comments"`
	Folder             string          `json:"folder"`
	Project            string          `json:"project"`
	Sections           json.RawMessage `json:"sections"`
	Subprojects        json.RawMessage `json:"subprojects"`
	Tasks              json.RawMessage `json:"tasks"`
}

type T_MigratePersonalTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type T_PlanDetailsResponse struct {
	CancelAtPeriodEnd              bool            `json:"cancel_at_period_end"`
	CurrentActiveProjects          int             `json:"current_active_projects"`
	CurrentMemberCount             int             `json:"current_member_count"`
	CurrentPlan                    string          `json:"current_plan"`
	CurrentPlanStatus              string          `json:"current_plan_status"`
	DowngradeAt                    string          `json:"downgrade_at"`
	HasBillingPortal               bool            `json:"has_billing_portal"`
	HasBillingPortalSwitchToAnnual bool            `json:"has_billing_portal_switch_to_annual"`
	HasTrialed                     bool            `json:"has_trialed"`
	IsTrialing                     bool            `json:"is_trialing"`
	MaximumActiveProjects          int             `json:"maximum_active_projects"`
	PlanPrice                      string          `json:"plan_price"`
	PriceList                      json.RawMessage `json:"price_list"`
	TrialEndsAt                    string          `json:"trial_ends_at"`
	WorkspaceId                    int             `json:"workspace_id"`
}

type T_ProjectsResponse struct {
	HasMore           bool            `json:"has_more"`
	NextCursor        string          `json:"next_cursor"`
	WorkspaceProjects json.RawMessage `json:"workspace_projects"`
}

type T_StatusOkResponse struct {
	Status string `json:"status"`
}

type T_UsersResponse struct {
	HasMore        bool            `json:"has_more"`
	NextCursor     string          `json:"next_cursor"`
	WorkspaceUsers json.RawMessage `json:"workspace_users"`
}

type TasksCompletedDateResponse struct {
	Items      json.RawMessage `json:"items"`
	NextCursor string          `json:"next_cursor"`
}

type TeamProjectConfiguration struct {
}

type UploadResult struct {
	FileName     string `json:"file_name"`
	FileSize     int    `json:"file_size"`
	FileType     string `json:"file_type"`
	FileUrl      string `json:"file_url"`
	Image        string `json:"image"`
	ImageHeight  string `json:"image_height"`
	ImageWidth   string `json:"image_width"`
	ResourceType string `json:"resource_type"`
	UploadState  string `json:"upload_state"`
}

type UrgentReminderDeviceView struct {
	DeviceId       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	DevicePlatform string `json:"device_platform"`
	DeviceToken    string `json:"device_token"`
}

type UserJSON struct {
	ActivatedUser               bool            `json:"activated_user"`
	AutoReminder                int             `json:"auto_reminder"`
	AvatarBig                   string          `json:"avatar_big"`
	AvatarMedium                string          `json:"avatar_medium"`
	AvatarS640                  string          `json:"avatar_s640"`
	AvatarSmall                 string          `json:"avatar_small"`
	BusinessAccountId           string          `json:"business_account_id"`
	CompletedCount              int             `json:"completed_count"`
	CompletedToday              int             `json:"completed_today"`
	DailyGoal                   int             `json:"daily_goal"`
	DateFormat                  int             `json:"date_format"`
	DaysOff                     json.RawMessage `json:"days_off"`
	DeletedAt                   string          `json:"deleted_at"`
	Email                       string          `json:"email"`
	FeatureIdentifier           string          `json:"feature_identifier"`
	Features                    json.RawMessage `json:"features"`
	FreeTrialExpires            string          `json:"free_trial_expires"`
	FullName                    string          `json:"full_name"`
	GettingStartedGuideProjects string          `json:"getting_started_guide_projects"`
	HasMagicNumber              bool            `json:"has_magic_number"`
	HasPassword                 bool            `json:"has_password"`
	HasStartedATrial            bool            `json:"has_started_a_trial"`
	Id                          string          `json:"id"`
	ImageId                     string          `json:"image_id"`
	InboxProjectId              string          `json:"inbox_project_id"`
	IsCelebrationsEnabled       bool            `json:"is_celebrations_enabled"`
	IsDeleted                   bool            `json:"is_deleted"`
	IsOnboarded                 string          `json:"is_onboarded"`
	IsPremium                   bool            `json:"is_premium"`
	JoinableWorkspace           string          `json:"joinable_workspace"`
	JoinedAt                    string          `json:"joined_at"`
	Karma                       float64         `json:"karma"`
	KarmaTrend                  string          `json:"karma_trend"`
	Lang                        string          `json:"lang"`
	MfaEnabled                  bool            `json:"mfa_enabled"`
	NextWeek                    int             `json:"next_week"`
	OnboardedDatedTasksCreated  string          `json:"onboarded_dated_tasks_created"`
	OnboardedDesktopAccessed    string          `json:"onboarded_desktop_accessed"`
	OnboardedMobileAccessed     string          `json:"onboarded_mobile_accessed"`
	OnboardedTasksCompleted     string          `json:"onboarded_tasks_completed"`
	OnboardingCompleted         bool            `json:"onboarding_completed"`
	OnboardingInitiated         bool            `json:"onboarding_initiated"`
	OnboardingLevel             string          `json:"onboarding_level"`
	OnboardingPersona           string          `json:"onboarding_persona"`
	OnboardingRole              string          `json:"onboarding_role"`
	OnboardingStarted           bool            `json:"onboarding_started"`
	OnboardingTeamMode          string          `json:"onboarding_team_mode"`
	OnboardingUseCases          string          `json:"onboarding_use_cases"`
	PremiumStatus               string          `json:"premium_status"`
	PremiumUntil                string          `json:"premium_until"`
	ShareLimit                  int             `json:"share_limit"`
	SortOrder                   int             `json:"sort_order"`
	StartDay                    int             `json:"start_day"`
	StartPage                   string          `json:"start_page"`
	ThemeId                     string          `json:"theme_id"`
	TimeFormat                  string          `json:"time_format"`
	Token                       string          `json:"token"`
	TzInfo                      json.RawMessage `json:"tz_info"`
	UrgentReminderDevice        string          `json:"urgent_reminder_device"`
	VerificationStatus          string          `json:"verification_status"`
	WebsocketUrl                string          `json:"websocket_url"`
	WeekendStartDay             int             `json:"weekend_start_day"`
	WeeklyGoal                  int             `json:"weekly_goal"`
}

type ValidationError struct {
	Ctx   json.RawMessage `json:"ctx"`
	Input string          `json:"input"`
	Loc   json.RawMessage `json:"loc"`
	Msg   string          `json:"msg"`
	Type  string          `json:"type"`
}

type WeeklyCompletionItem struct {
	From           string          `json:"from"`
	Items          json.RawMessage `json:"items"`
	To             string          `json:"to"`
	TotalCompleted int             `json:"total_completed"`
}

type WorkspaceInvitationView struct {
	Id             string `json:"id"`
	InviterId      string `json:"inviter_id"`
	IsExistingUser bool   `json:"is_existing_user"`
	Role           string `json:"role"`
	UserEmail      string `json:"user_email"`
	WorkspaceId    string `json:"workspace_id"`
}

type WorkspaceProjectSyncView struct {
	Access                              string          `json:"access"`
	CanAssignTasks                      bool            `json:"can_assign_tasks"`
	CanComment                          bool            `json:"can_comment"`
	ChildOrder                          int             `json:"child_order"`
	CollaboratorRoleDefault             string          `json:"collaborator_role_default"`
	Color                               string          `json:"color"`
	CreatedAt                           string          `json:"created_at"`
	CreatorUid                          string          `json:"creator_uid"`
	DefaultOrder                        int             `json:"default_order"`
	Description                         string          `json:"description"`
	FolderId                            string          `json:"folder_id"`
	GoalIds                             json.RawMessage `json:"goal_ids"`
	Id                                  string          `json:"id"`
	IsArchived                          bool            `json:"is_archived"`
	IsCollapsed                         bool            `json:"is_collapsed"`
	IsDeleted                           bool            `json:"is_deleted"`
	IsFavorite                          bool            `json:"is_favorite"`
	IsFrozen                            bool            `json:"is_frozen"`
	IsInviteOnly                        string          `json:"is_invite_only"`
	IsLinkSharingEnabled                bool            `json:"is_link_sharing_enabled"`
	IsPendingDefaultCollaboratorInvites bool            `json:"is_pending_default_collaborator_invites"`
	IsProjectInsightsEnabled            bool            `json:"is_project_insights_enabled"`
	IsShared                            bool            `json:"is_shared"`
	Name                                string          `json:"name"`
	PublicKey                           string          `json:"public_key"`
	Role                                string          `json:"role"`
	Status                              string          `json:"status"`
	UpdatedAt                           string          `json:"updated_at"`
	ViewStyle                           string          `json:"view_style"`
	WorkspaceId                         string          `json:"workspace_id"`
}

type WorkspaceProjectView struct {
	Access                   string `json:"access"`
	ArchivedDate             string `json:"archived_date"`
	ArchivedTimestamp        int    `json:"archived_timestamp"`
	Color                    string `json:"color"`
	DefaultOrder             int    `json:"default_order"`
	Description              string `json:"description"`
	FolderId                 string `json:"folder_id"`
	InitiatedByUid           int    `json:"initiated_by_uid"`
	IsArchived               bool   `json:"is_archived"`
	IsFrozen                 bool   `json:"is_frozen"`
	IsInviteOnly             string `json:"is_invite_only"`
	IsProjectInsightsEnabled bool   `json:"is_project_insights_enabled"`
	Name                     string `json:"name"`
	ProjectId                string `json:"project_id"`
	PublicAccess             bool   `json:"public_access"`
	Status                   string `json:"status"`
	ViewStyle                string `json:"view_style"`
	WorkspaceId              int    `json:"workspace_id"`
}

type WorkspaceProperties struct {
	AcquisitionLoopLastShow string `json:"acquisition_loop_last_show"`
	AcquisitionSource       string `json:"acquisition_source"`
	BetaEnabled             bool   `json:"beta_enabled"`
	Country                 string `json:"country"`
	CreatedPlatform         string `json:"created_platform"`
	CreatorRole             string `json:"creator_role"`
	DefaultAccessLevel      string `json:"default_access_level"`
	Department              string `json:"department"`
	DesktopWorkspaceModal   string `json:"desktop_workspace_modal"`
	Hdyhau                  string `json:"hdyhau"`
	Industry                string `json:"industry"`
	OnboardingFilterId      string `json:"onboarding_filter_id"`
	OrganizationSize        string `json:"organization_size"`
	Region                  string `json:"region"`
	TeamAcquisitionCohort   string `json:"team_acquisition_cohort"`
}

type WorkspaceUserView struct {
	CustomSortingApplied  bool   `json:"custom_sorting_applied"`
	ProjectSortPreference string `json:"project_sort_preference"`
	Role                  string `json:"role"`
	UserId                string `json:"user_id"`
	WorkspaceId           string `json:"workspace_id"`
}

type WorkspacesGetItem struct {
}
