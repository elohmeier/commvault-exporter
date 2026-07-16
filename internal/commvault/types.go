package commvault

type LoginResponse struct {
	Token     string `json:"token"`
	AuthToken string `json:"authToken"`
	Data      struct {
		Token     string `json:"token"`
		AuthToken string `json:"authToken"`
	} `json:"data"`
	ErrorCode      int          `json:"errorCode"`
	ErrorMessage   string       `json:"errorMessage"`
	ErrLogMessage  string       `json:"errLogMessage"`
	Message        string       `json:"message"`
	LoginAttempts  int          `json:"loginAttempts"`
	RemainingLock  int          `json:"remainingLockTime"`
	ForcePwdChange bool         `json:"forcePasswordChange"`
	AccountLocked  bool         `json:"isAccountLocked"`
	ErrList        []LoginError `json:"errList"`
}

type LoginError struct {
	ErrorCode     int    `json:"errorCode"`
	ErrorMessage  string `json:"errorMessage"`
	ErrLogMessage string `json:"errLogMessage"`
	Message       string `json:"message"`
}

type LicenseInfoResponse struct {
	CommCellID         int64  `json:"commCellId"`
	CommServeIPAddress string `json:"commServeIPAddress"`
	LicenseIPAddress   string `json:"licenseIPAddress"`
	Edition            string `json:"edition"`
	LicenseMode        string `json:"licenseMode"`
	SerialNumber       string `json:"serialNumber"`
	RegistrationCode   string `json:"registrationCode"`
	ExpiryDate         int64  `json:"expiryDate"`
}

type VMResponse struct {
	TotalRecords     int      `json:"totalRecords"`
	PageNo           int      `json:"pageNo"`
	PageSize         int      `json:"pageSize"`
	ErrorMessage     string   `json:"errorMessage"`
	ErrorCode        int      `json:"errorCode"`
	VMStatusInfoList []VMInfo `json:"vmStatusInfoList"`
}

type VMInfo struct {
	VMHost       string `json:"vmHost"`
	VMGuestSpace int64  `json:"vmGuestSpace"`
	Type         int    `json:"type"`
	VMStatus     int    `json:"vmStatus"`
	OSName       string `json:"strOSName"`
	IsDeleted    bool   `json:"isDeleted"`
	Vendor       int    `json:"vendor"`
	OSType       int    `json:"osType"`
	VMSize       int64  `json:"vmSize"`
	VMUsedSpace  int64  `json:"vmUsedSpace"`
	SubclientID  int64  `json:"subclientId"`
	VMAgent      string `json:"vmAgent"`
	Name         string `json:"name"`
	HardwareVer  string `json:"vmHardwareVer"`
	GUID         string `json:"strGUID"`
	Subclient    string `json:"subclientName"`
	Client       Entity `json:"client"`
	ProxyClient  Entity `json:"proxyClient"`
	PseudoClient Entity `json:"pseudoClient"`
	Plan         struct {
		PlanName string `json:"planName"`
		PlanID   int64  `json:"planId"`
	} `json:"plan"`
	LastBackupJobInfo VMBackupJobInfo `json:"lastBackupJobInfo"`
}

type Entity struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ClientID   int64  `json:"clientId"`
	ClientName string `json:"clientName"`
}

type VMBackupJobInfo struct {
	JobID     int64         `json:"jobID"`
	StartTime CommvaultTime `json:"startTime"`
	EndTime   CommvaultTime `json:"endTime"`
}

type CommvaultTime struct {
	Time int64 `json:"time"`
}

func (e Entity) EntityID() int64 {
	if e.ID != 0 {
		return e.ID
	}
	return e.ClientID
}

func (e Entity) EntityName() string {
	if e.Name != "" {
		return e.Name
	}
	return e.ClientName
}

type TabularResponse struct {
	TotalRecordCount int              `json:"totalRecordCount"`
	RecordsCount     int              `json:"recordsCount"`
	Columns          []TabularColumn  `json:"columns"`
	Records          [][]interface{}  `json:"records"`
	Failures         map[string]any   `json:"failures"`
	Warnings         map[string]any   `json:"warnings"`
	RawRecords       []map[string]any `json:"-"`
}

type TabularColumn struct {
	Name     string `json:"name"`
	Data     string `json:"dataField"`
	Display  string `json:"displayName"`
	Type     string `json:"type"`
	Precison int    `json:"precision"`
	Scale    int    `json:"scale"`
}

type JobsResponse struct {
	TotalRecordsWithoutPaging int       `json:"totalRecordsWithoutPaging"`
	Jobs                      []JobItem `json:"jobs"`
}

type JobItem struct {
	Summary JobSummary `json:"jobSummary"`
}

type JobSummary struct {
	SizeOfApplication      int64     `json:"sizeOfApplication"`
	BackupSetName          string    `json:"backupSetName"`
	TotalFailedFolders     int64     `json:"totalFailedFolders"`
	TotalFailedFiles       int64     `json:"totalFailedFiles"`
	LocalizedStatus        string    `json:"localizedStatus"`
	TotalNumOfFiles        int64     `json:"totalNumOfFiles"`
	JobID                  int64     `json:"jobId"`
	JobSubmitErrorCode     int64     `json:"jobSubmitErrorCode"`
	SizeOfMediaOnDisk      int64     `json:"sizeOfMediaOnDisk"`
	Status                 string    `json:"status"`
	LastUpdateTime         int64     `json:"lastUpdateTime"`
	PercentSavings         float64   `json:"percentSavings"`
	LocalizedOperationName string    `json:"localizedOperationName"`
	StatusColor            string    `json:"statusColor"`
	BackupLevel            int64     `json:"backupLevel"`
	JobElapsedTime         int64     `json:"jobElapsedTime"`
	JobStartTime           int64     `json:"jobStartTime"`
	JobType                string    `json:"jobType"`
	BackupLevelName        string    `json:"backupLevelName"`
	AppTypeName            string    `json:"appTypeName"`
	PercentComplete        float64   `json:"percentComplete"`
	SubclientName          string    `json:"subclientName"`
	DestClientName         string    `json:"destClientName"`
	CurrentPhaseName       string    `json:"currentPhaseName"`
	Subclient              Subclient `json:"subclient"`
}

type Subclient struct {
	ClientName    string `json:"clientName"`
	InstanceName  string `json:"instanceName"`
	BackupsetID   int64  `json:"backupsetId"`
	InstanceID    int64  `json:"instanceId"`
	SubclientID   int64  `json:"subclientId"`
	ClientID      int64  `json:"clientId"`
	AppName       string `json:"appName"`
	BackupsetName string `json:"backupsetName"`
	ApplicationID int64  `json:"applicationId"`
	SubclientName string `json:"subclientName"`
}

type AlertsResponse struct {
	MyReceiveTotal int           `json:"myReceiveTotal"`
	MyCreatedTotal int           `json:"myCreatedTotal"`
	AlertList      []AlertConfig `json:"alertList"`
}

type AlertConfig struct {
	NotifType     int    `json:"notifType"`
	CreatedTime   int64  `json:"createdTime"`
	Status        int64  `json:"status"`
	Creator       IDName `json:"creator"`
	AlertType     IDName `json:"alertType"`
	Alert         IDName `json:"alert"`
	AlertCategory IDName `json:"alertCategory"`
}

type TriggeredAlertsResponse struct {
	TotalCount      int              `json:"totalCount"`
	UnreadCount     int              `json:"unreadCount"`
	AlertsTriggered []TriggeredAlert `json:"alertsTriggered"`
}

type TriggeredAlert struct {
	ID                int64  `json:"id"`
	Severity          string `json:"severity"`
	DetectedCriterion string `json:"detectedCriterion"`
	Type              string `json:"type"`
	DetectedTime      int64  `json:"detectedTime"`
	Client            IDName `json:"client"`
	ReadStatus        bool   `json:"readStatus"`
	PinStatus         bool   `json:"pinStatus"`
	JobID             int64  `json:"jobId"`
	Company           IDName `json:"company"`
	Commcell          struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"commcell"`
}

type IDName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type StoragePoolsResponse struct {
	StoragePoolList []StoragePool `json:"storagePoolList"`
}

type StoragePool struct {
	NumberOfNodes     int64  `json:"numberOfNodes"`
	TotalFreeSpace    int64  `json:"totalFreeSpace"`
	StoragePoolType   int64  `json:"storagePoolType"`
	TotalCapacity     int64  `json:"totalCapacity"`
	Status            string `json:"status"`
	StatusCode        int64  `json:"statusCode"`
	StoragePoolEntity struct {
		Name string `json:"storagePoolName"`
		ID   int64  `json:"storagePoolId"`
	} `json:"storagePoolEntity"`
	StoragePool struct {
		ClientGroupID   int64  `json:"clientGroupId"`
		ClientGroupName string `json:"clientGroupName"`
	} `json:"storagePool"`
}

type StoragePoliciesResponse struct {
	Policies []StoragePolicy `json:"policies"`
}

type StoragePolicy struct {
	Type             int64 `json:"type"`
	NumberOfStreams  int64 `json:"numberOfStreams"`
	NumberOfCopies   int64 `json:"numberOfCopies"`
	StoragePolicyRef struct {
		Name string `json:"storagePolicyName"`
		ID   int64  `json:"storagePolicyId"`
	} `json:"storagePolicy"`
	Plans []struct {
		Name string `json:"planName"`
		ID   int64  `json:"planId"`
	} `json:"plans"`
}

type MediaAgentsResponse struct {
	Response []struct {
		EntityInfo IDName `json:"entityInfo"`
	} `json:"response"`
}
