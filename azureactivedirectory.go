package itswizard_m_azureactivedirctory

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/itslearninggermany/itswizard_m_msgraph"
	"github.com/itslearninggermany/itswizard_m_s3bucket"
	"github.com/itslearninggermany/itswizard_m_sync"
	"github.com/jinzhu/gorm"
	msgraph "github.com/yaegashi/msgraph.go/beta"
	"log"
	//"regexp"
	"strconv"
	"strings"
	"sync"
	//"time"
)

/*
In the struct the information and the syncing will take place.
*/
type AadCrawlerSetup struct {
	gorm.Model
	//user Information
	OrganisationID uint `gorm:"type:varchar(100)"`
	InstitutionID  uint `gorm:"type:varchar(100);unique"`
	//Connect to database
	aadCrawlerDatabase *gorm.DB `gorm:"-"`
	clientDatabase     *gorm.DB `gorm:"-"`
	//Connect to msGraph
	TenantID      string       `gorm:"type:varchar(100)"`
	ApplicationID string       `gorm:"type:varchar(100)"`
	ClientSecret  string       `gorm:"type:varchar(100)"`
	msGraph       *GraphClient `gorm:"-"`
	// Special Fields:
	//	FieldForChildIDs   string `gorm:"type:varchar(100)"`
	//	FieldForMentorIDs  string `gorm:"type:varchar(100)"`
	FieldForProfileIDs string `gorm:"type:varchar(100)"`
	// Sync ID
	//	IdOfSyncGroup string `gorm:"type:varchar(100)"`
	// For syncing Process
	mux sync.Mutex `gorm:"-"`
	// All Data for the Sync:
	//AAD
	personsFromAad    []itswizard_m_sync.DbPerson15                    `gorm:"-"`
	groupsFromAad     []itswizard_m_sync.DbGroup15                     `gorm:"-"`
	membershipFromAad []itswizard_m_sync.DbGroupMembership15           `gorm:"-"`
	msrFromAad        []itswizard_m_sync.DbMentorStudentRelationship15 `gorm:"-"`
	sprFromAad        []itswizard_m_sync.DbStudentParentRelationship15 `gorm:"-"`
	// Institution
	personsFromInstitutionDatabase []itswizard_m_sync.DbPerson15 `gorm:"-"`
	groupsFromInstitutionDatabase  []itswizard_m_sync.DbGroup15  `gorm:"-"`
	//Organisation
	personsFromOrganisationDatabase []itswizard_m_sync.DbPerson15                    `gorm:"-"`
	groupsFromOrganisaionDatabase   []itswizard_m_sync.DbGroup15                     `gorm:"-"`
	sprFromOrganisationDatabase     []itswizard_m_sync.DbStudentParentRelationship15 `gorm:"-"`
	msrFromOrganisationDatabase     []itswizard_m_sync.DbMentorStudentRelationship15 `gorm:"-"`
	membershipFromDatabase          []itswizard_m_sync.DbGroupMembership15           `gorm:"-"`
	//syncCache
	Synccache itswizard_m_sync.AadSyncCache `gorm:"-"`
}

type AAdSyncCacheStore struct {
	gorm.Model
	OrganisationID uint
	Filename       string
}

type AadGroupsNotToSync struct {
	gorm.Model
	OrganisationID uint   `gorm:"unique"`
	RegEx          string `gorm:"type:varchar(500)"`
}

type AadCourseRegEx struct {
	gorm.Model
	OrganisationID uint   `gorm:"unique"`
	RegEx          string `gorm:"type:varchar(500)"`
}

/*
For choice wich user will be get
*/
type AadCrawlerUsernameSelect struct {
	gorm.Model
	OrganisationID uint   `json:"organisation_id"`
	Fieldname      string `json:"fieldname"` // Fieldname in AAD
	Operation      string `json:"operation"` // ==, !=
	Like           bool   `json:"like"`      // Like true then it is inside; False: Value must be the same in the AAD
	Value          string `json:"value"`     // Value like: @exapmle.com
}

/*
Start the Sync
*/
func (p *AadCrawlerSetup) Sync() error {
	log.Println("Start sync for Orga: ", p.OrganisationID)
	p.startSync()
	log.Println("Set All Data For Sync for Orga: ", p.OrganisationID)
	err := p.setAllDataForSync()
	if err != nil {
		log.Println(err)
	}
	log.Println("Run Sync Methode for: ", p.OrganisationID)
	p.SyncMethod()
	log.Println("Saving SyncCache-File")
	b, err := json.Marshal(p.Synccache)
	if err != nil {
		log.Println("Problem by storing a file to itslearning")
		return err
	}
	bucket, err := itswizard_m_s3bucket.NewBucket("itswizard", "eu-central-1")
	if err != nil {
		log.Println("Problem by creating Bucket type")
		return err
	}
	files, err := bucket.ListAllFiles("aad/syncCache/")
	if err != nil {
		log.Println("Problem by reading files in Bucket: ", err)
	}
	var filename string
	inList := true
	for inList {
		inList = false
		filename = uuid.New().String() + ".json"
		for i := 0; i < len(files); i++ {
			if files[i] == filename {
				inList = true
				break
			}
		}
	}
	err = bucket.ContentUpload("aad/syncCache/"+strconv.Itoa(int(p.InstitutionID))+"/"+filename, b)
	if err != nil {
		log.Println("Problem by uploading content")
		return err
	}
	p.aadCrawlerDatabase.Save(&AAdSyncCacheStore{
		OrganisationID: p.OrganisationID,
		Filename:       filename,
	})
	p.Synccache.RunCache(p.aadCrawlerDatabase, p.clientDatabase, p.OrganisationID, p.InstitutionID)
	p.stopSync()
	log.Println("All finished ...... <>")
	return nil
}

/*
Sync Method
*/
func (p *AadCrawlerSetup) SyncMethod() {
	log.Println("Start Sync Method")
	p.Synccache = itswizard_m_sync.SyncMethodAAD(p.InstitutionID, p.OrganisationID, p.personsFromAad, p.msrFromAad, p.sprFromAad, p.personsFromInstitutionDatabase, p.personsFromOrganisationDatabase, p.sprFromOrganisationDatabase, p.msrFromOrganisationDatabase, p.membershipFromDatabase, p.membershipFromAad, p.groupsFromAad, p.groupsFromOrganisaionDatabase)
}

/*
Makes a new AadCrawlerSetup and set the
  - database
  - user Information
  - special Fields
  - mGraph
*/
func NewAadCrawlerSetup(aadCrwalerDatabase *gorm.DB, clientDatabase *gorm.DB, OrganisationId uint) (out *AadCrawlerSetup, err error) {
	log.Println("Setup the AAD Crawler")
	aad := new(AadCrawlerSetup)
	aad.aadCrawlerDatabase = aadCrwalerDatabase
	aad.clientDatabase = clientDatabase
	err = aad.aadCrawlerDatabase.Where("organisation_id = ?", OrganisationId).First(&aad).Error
	if err != nil {
		return nil, err
	}
	aad.msGraph, err = NewGraphClient(aad.TenantID, aad.ApplicationID, aad.ClientSecret)
	return aad, nil
}

/*
Locks the mutex
*/
func (a *AadCrawlerSetup) startSync() {
	log.Println("Lock the process with mutex")
	a.mux.Lock()
}

/*
Unlocks the mutex
*/
func (a *AadCrawlerSetup) stopSync() {
	log.Println("Unlock the process with mutex")
	a.mux.Unlock()
}

/*
UserprincipalNames works atm not more
*/
func personToSync(userSelect []AadCrawlerUsernameSelect, user msgraph.User) bool {
	toSync := false
	for i := 0; i < len(userSelect); i++ {
		if userSelect[i].Fieldname == "UserPrincipalName" {
			if userSelect[i].Operation == "==" {
				if userSelect[i].Like {
					if strings.Contains(itswizard_m_msgraph.UnPtrString(user.UserPrincipalName), userSelect[i].Value) {
						toSync = true
					}
				}
				if !userSelect[i].Like {
					if itswizard_m_msgraph.UnPtrString(user.UserPrincipalName) == userSelect[i].Value {
						toSync = true
					}
				}
			}
			if userSelect[i].Operation == "!=" {
				if userSelect[i].Like {
					if !strings.Contains(itswizard_m_msgraph.UnPtrString(user.UserPrincipalName), userSelect[i].Value) {
						toSync = true
					}
				}
				if !userSelect[i].Like {
					if !(itswizard_m_msgraph.UnPtrString(user.UserPrincipalName) == userSelect[i].Value) {
						toSync = true
					}
				}
			}
		}
	}
	return toSync
}

/*
Get Profile
*/
type profileSetting struct {
	Id         int               `json:"id"` // 1 ==> Groups; 2 ==> Fieldname; 3 ==> Domainname
	Groups     map[string]string `json:"groups"`
	Fieldname  string            `json:"fieldname"`
	Domainname map[string]string `json:"domainname"`
}

/*
By ID 2 only department and jobtitle works
*/
func getProfile(fieldForProfileIDs string, user msgraph.User, groupsIds map[string]string) string {
	var profileSetting profileSetting
	err := json.Unmarshal([]byte(fieldForProfileIDs), &profileSetting)
	if err != nil {
		log.Println("Problem by unmarshalling profileStting: ", err)
		return "Guest"
	}

	switch profileSetting.Id {
	// Groups
	case 1:
		profile := groupsIds[itswizard_m_msgraph.UnPtrString(user.ID)]
		if profile == "" {
			return "Guest"
		} else {
			return profile
		}

	// Fieldname
	case 2:
		switch profileSetting.Fieldname {
		case "jobTitle":
			if user.JobTitle == nil {
				return "Guest"
			} else {
				return itswizard_m_msgraph.UnPtrString(user.JobTitle)
			}
		case "department":
			if user.Department == nil {
				return "Guest"
			} else {
				return itswizard_m_msgraph.UnPtrString(user.Department)
			}
		default:
			return "Guest"
		}

	// Domainname
	case 3:
		//		["@irgendwas.de"]"staff"
		for key, value := range profileSetting.Domainname {
			if strings.Contains(itswizard_m_msgraph.UnPtrString(user.UserPrincipalName), key) {
				return value
			}
		}
		return "Guest"
	default:
		return "Guest"
	}
}

/*
Important/ Wichtig: Profile findet sind sich im Jobtitle.
*/
func (a *AadCrawlerSetup) SetPersonsFromAad() (err error) {
	log.Println("Downloading Persons from AAD")

	var userSelect []AadCrawlerUsernameSelect
	err = a.aadCrawlerDatabase.Where("organisation_id = ?", a.OrganisationID).Find(&userSelect).Error
	if err != nil {
		log.Println("Problem with getting Aad Crawler Username Select")
		return err
	}

	var out []itswizard_m_sync.DbPerson15
	/*
		Todo: Use other graph API
	*/
	aadAction := itswizard_m_msgraph.NewAADAction(a.TenantID, a.ApplicationID, a.ClientSecret)
	allUsers, err := aadAction.GetAllUsers()
	if err != nil {
		return err
	}

	//ProfileIDs
	var profileSetting profileSetting
	err = json.Unmarshal([]byte(a.FieldForProfileIDs), &profileSetting)
	if err != nil {
		log.Println("Problem by unmarshalling profileStting: ", err)
		return err
	}

	groupsIds := make(map[string]string)
	if profileSetting.Id == 1 {
		//[key]value
		for key, value := range profileSetting.Domainname {
			groupmembers, err := aadAction.GetAllMembersOfAGroup(key)
			if err != nil {
				log.Println("Problem by reading AllMembersOfGroup from aad by profile setting")
				return err
			}
			for i := 0; i < len(groupmembers); i++ {
				groupsIds[itswizard_m_msgraph.UnPtrString(groupmembers[i].ID)] = value
			}
		}

	}

	for i := 0; i < len(allUsers); i++ {
		// Check if Person is to sync
		if !personToSync(userSelect, allUsers[i]) {
			log.Println("Person is not to sync", itswizard_m_msgraph.UnPtrString(allUsers[i].UserPrincipalName))
			continue
		}

		out = append(out, itswizard_m_sync.DbPerson15{
			ID:                 fmt.Sprint(itswizard_m_msgraph.UnPtrString(allUsers[i].ID), "++", a.OrganisationID),
			SyncPersonKey:      itswizard_m_msgraph.UnPtrString(allUsers[i].ID),
			FirstName:          itswizard_m_msgraph.UnPtrString(allUsers[i].GivenName),
			LastName:           itswizard_m_msgraph.UnPtrString(allUsers[i].Surname),
			Username:           itswizard_m_msgraph.UnPtrString(allUsers[i].UserPrincipalName),
			Email:              itswizard_m_msgraph.UnPtrString(allUsers[i].Mail),
			Profile:            getProfile(a.FieldForProfileIDs, allUsers[i], groupsIds),
			Mobile:             itswizard_m_msgraph.UnPtrString(allUsers[i].MobilePhone),
			Street1:            itswizard_m_msgraph.UnPtrString(allUsers[i].StreetAddress),
			Street2:            itswizard_m_msgraph.UnPtrString(allUsers[i].Country),
			Postcode:           itswizard_m_msgraph.UnPtrString(allUsers[i].PostalCode),
			City:               itswizard_m_msgraph.UnPtrString(allUsers[i].City),
			DbOrganisation15ID: a.OrganisationID,
			DbInstitution15ID:  a.InstitutionID,
			AzureUserID:        itswizard_m_msgraph.UnPtrString(allUsers[i].ID),
		})
	}

	a.personsFromAad = out
	log.Println("Finished Downloading Perosns from AAD")
	return nil
}

/*
Todo: ElternKind und MentorStudent Relationship muss noch erstellet werden!
*/
func (p *AadCrawlerSetup) setMsrFromAad() (err error) {
	log.Println("Set MSR - Not activated")
	p.msrFromAad = nil
	//Todo
	return nil
}

/*
Todo: ElternKind und MentorStudent Relationship muss noch erstellet werden!
*/
func (p *AadCrawlerSetup) setSprFromAad() (err error) {
	log.Println("Set SPR - Not activated")
	p.sprFromAad = nil
	//Todo
	return nil
}

/*
sets the Persons of the Institution to the struct
*/
func (p *AadCrawlerSetup) setPersonsFromInstitutionDatabase() (err error) {
	log.Println("Getting all Persons from Database of the institution")
	err = p.clientDatabase.Where("db_institution15_id = ?", p.InstitutionID).Find(&p.personsFromInstitutionDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished getting all Persons from Database of the institution")
	return nil
}

/*
sets the Persons of the Organisation to the struct
*/
func (p *AadCrawlerSetup) setPersonsFromOrganisationDatabase() (err error) {
	log.Println("Getting all Persons from Database of the organisation")
	err = p.clientDatabase.Where("db_institution15_id = ? AND db_organisation15_id = ?", p.InstitutionID, p.OrganisationID).Find(&p.personsFromOrganisationDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished getting all Persons from Database of the organisation")
	return nil
}

/*
sets the Student Person Relationships from the Organisation to the struct
*/
func (p *AadCrawlerSetup) setSprFromOrganisationDatabase() (err error) {
	log.Println("Getting all SPR from Database of the institution")
	err = p.clientDatabase.Where("db_institution15_id = ? AND db_organisation15_id = ?", p.InstitutionID, p.OrganisationID).Find(&p.sprFromOrganisationDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished getting all SPR from Database of the institution")
	return nil
}

/*
sets Mentor Student Relationship of the Organisation to the database
*/
func (p *AadCrawlerSetup) setMsrFromOrganisationDatabase() (err error) {
	log.Println("Getting all MSR from Database of the institution")
	err = p.clientDatabase.Where("db_institution15_id = ? AND db_organisation15_id = ?", p.InstitutionID, p.OrganisationID).Find(&p.msrFromOrganisationDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished getting all MSR from Database of the institution")
	return nil
}

/*
set the Membership from the AAD to the struct
*/
func (p *AadCrawlerSetup) setMembershipFromAad() (err error) {
	log.Println("Get all Memberships from AAD")
	for in := 0; in < len(p.groupsFromAad); in++ {
		group, err := p.msGraph.GetGroup(p.groupsFromAad[in].SyncID)
		if err != nil {
			return err
		}
		members, err := group.ListMembers()
		if err != nil {
			return err
		}

		for i := 0; i < len(members); i++ {
			p.membershipFromAad = append(p.membershipFromAad, itswizard_m_sync.DbGroupMembership15{
				ID:                 fmt.Sprint(p.InstitutionID, "++", p.OrganisationID, "++", fmt.Sprint(p.InstitutionID, "++", p.OrganisationID, "++", group.ID, "++", members[i].ID)),
				PersonSyncKey:      members[i].ID,
				GroupName:          fmt.Sprint(group.ID),
				DbInstitution15ID:  p.InstitutionID,
				DbOrganisation15ID: p.OrganisationID,
			})
		}
	}
	log.Println("Finished get all Memberships from AAD")
	return nil
}

/*
set the Membership from Database in the database
*/
func (p *AadCrawlerSetup) setMembershipFromOrganisationDatabase() (err error) {
	log.Println("Getting all Memberships from Database of the organisation")
	err = p.clientDatabase.Where("db_institution15_id = ? AND db_organisation15_id = ?", p.InstitutionID, p.OrganisationID).Find(&p.membershipFromDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished getting all Memberships from Database of the organisation")
	return nil
}

/*
Get the Gropus from the aad and set it in the struct
setPersonsFromAAD must called before
*/
/*
Get the Gropus from the aad and set it in the struct
setPersonsFromAAD must called before
*/
/*
func (p *AadCrawlerSetup) setGroupsFromAad() (err error) {
	log.Println("Get all Groups from the aad")
	listOfGroupIds := make(map[string]bool)
	aadAction := itswizard_msgraph.NewAADAction(p.TenantID, p.ApplicationID, p.ClientSecret)
	for i := 0; i < len(p.personsFromAad); i++ {
		groupIDs, err := aadAction.GetMemberGroups(p.personsFromAad[i].SyncPersonKey)
		if err != nil {
			log.Println("Error by Get Member Groups from AAD")
		}
		for in := 0; in < len(groupIDs); in++ {
			listOfGroupIds[groupIDs[in]] = true
		}
		time.Sleep(10 * time.Millisecond)
	}

	var regExCourse AadCourseRegEx
	err = p.aadCrawlerDatabase.Where("organisation_id = ?", p.OrganisationID).Find(&regExCourse).Error
	if err != nil {
		log.Println("Error by Get regEx from Database")
	}

	var regExNotTyoSync AadGroupsNotToSync
	err = p.aadCrawlerDatabase.Where("organisation_id = ?", p.OrganisationID).Find(&regExNotTyoSync).Error
	if err != nil {
		log.Println("Error by Get regEx from Database")
	}

	groups, err := aadAction.ShowGroups()
	for i := 0; i < len(groups); i++ {
		fmt.Println(groups[i].DisplayName)
		var reg = regexp.MustCompile(regExNotTyoSync.RegEx)
		if reg.MatchString(itswizard_msgraph.UnPtrString(groups[i].DisplayName)) {
			continue
		}
		if listOfGroupIds[itswizard_msgraph.UnPtrString(groups[i].ID)] {
			var re = regexp.MustCompile(regExCourse.RegEx)
			course := false
			if re.MatchString(itswizard_msgraph.UnPtrString(groups[i].DisplayName)) {
				course = true
			} else {
				course = false
			}
			p.groupsFromAad = append(p.groupsFromAad, itswizard_sync.DbGroup15{
				ID:                 fmt.Sprint(p.InstitutionID, "++", p.OrganisationID, "++", itswizard_msgraph.UnPtrString(groups[i].ID)),
				SyncID:             itswizard_msgraph.UnPtrString(groups[i].ID),
				Name:               itswizard_msgraph.UnPtrString(groups[i].DisplayName),
				ParentGroupID:      "rootPointer",
				Level:              1,
				DbInstitution15ID:  p.InstitutionID,
				DbOrganisation15ID: p.OrganisationID,
				IsCourse:           course,
			})
		} else {
			log.Println("Not to sync: ", groups[i].DisplayName)
		}
	}
	log.Println("Finished get all Groups from the aad")
	fmt.Println(len(groups))
	return nil
}
*/

/*
Set the Groups from the Organisation to the struct
*/
func (p *AadCrawlerSetup) setGroupsFromOrganisaionDatabase() (err error) {
	log.Println("Get all Groups from Database of organisation")
	err = p.clientDatabase.Where("db_institution15_id = ? AND db_organisation15_id = ?", p.InstitutionID, p.OrganisationID).Find(&p.groupsFromOrganisaionDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished get all Groups from Database of organisation")
	return nil
}

/*
Set the Groups from the Institution to the struct
*/
func (p *AadCrawlerSetup) setGroupsFromInstitutionDatabase() (err error) {
	log.Println("Get all Groups from Database of institution")
	err = p.clientDatabase.Where("db_institution15_id = ?", p.InstitutionID).Find(&p.groupsFromOrganisaionDatabase).Error
	if err != nil {
		return err
	}
	log.Println("Finished get all Groups from Database of institution")
	return nil
}

/*
Set this data to the struct:
- Persons from AadCrawlerSetup
- Mentor Student Relationship AadCrawlerSetup
- Student Parent Relationship AadCrawlerSetup
- ..
*/
func (p *AadCrawlerSetup) setAllDataForSync() (err error) {
	//Data from AAD
	err = p.SetPersonsFromAad()
	if err != nil {
		return err
	}
	/*
		err = p.setGroupsFromAad()
		if err != nil {
			return err
		}
	*/
	err = p.setMembershipFromAad()
	if err != nil {
		return err
	}
	err = p.setMsrFromAad()
	if err != nil {
		return err
	}
	err = p.setSprFromAad()
	if err != nil {
		return err
	}
	//Data from Database (Institution)
	err = p.setPersonsFromInstitutionDatabase()
	if err != nil {
		return err
	}
	err = p.setGroupsFromInstitutionDatabase()
	if err != nil {
		return err
	}

	//Data from Database (Organisation)
	err = p.setPersonsFromOrganisationDatabase()
	if err != nil {
		return err
	}
	err = p.setGroupsFromOrganisaionDatabase()
	if err != nil {
		return err
	}
	err = p.setMembershipFromOrganisationDatabase()
	if err != nil {
		return err
	}
	err = p.setMsrFromOrganisationDatabase()
	if err != nil {
		return err
	}
	err = p.setSprFromOrganisationDatabase()
	if err != nil {
		return err
	}
	return nil
}

/*
Todo: To create
*/
func (p *AadCrawlerSetup) storeSynccacheToDatabase() error {

	return nil
}

/*
func (p *AadCrawlerSetup) UpdateLicenseGroup() {
	aad := itswizard_msgraph.NewAADAction(p.TenantID, p.ApplicationID, p.ClientSecret)
	for i := 0; i < len(p.synccache.PersonToImport); i++ {
		time.Sleep(25 * time.Millisecond)
		// Todo: Udaate to personToImport Azure ID
		err := aad.AddMemberToAGroup(p.IdOfSyncGroup, p.synccache.PersonToImport[i].SyncPersonKey)
		if err != nil {
			//Todo: ErrorHandling
			fmt.Println(err)
			continue
		}
	}
}
*/
