package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Magic values for JIRA export CSV field names
const fieldIssueID string = "Issue key"
const fieldIssueKey string = "Issue id"
const fieldIssueType string = "Issue Type"
const fieldStatus string = "Status"
const fieldCreated string = "Created"
const fieldResolved string = "Resolved"
const fieldLabels string = "Labels"
const fieldPoints string = "Custom field (Story point estimate)"
const fieldParentKey string = "Parent"

// Date formats
const jiraDate = "02/Jan/06 15:04 PM" // Format that JIRA uses
const isoDate = "2006-01-02"          // ISO 8601

// In memory backlog record structure
type backlogItem struct {
	itemType    string
	id          string
	parent      string
	hasChildren bool
	opened      time.Time
	closed      time.Time
	points      float64
	tags        string
}

// Dynamically determined column IDs for attributes in CSV import file
var ndxIssueID int   // ID
var ndxIssueKey int  // Unique record ID
var ndxIssueType int // Type (bug, defect, epic, etc.)
var ndxStatus int    // Status (in progress, done, etc.)
var ndxCreated int   // Date created
var ndxResolved int  // Date resolved
var ndxLabels int    // Labels or tags
var ndxPoints int    // Story points
var ndxParentKey int // Parent's unique record ID

// Create a directory if it does not already exist
// c.f.  https://siongui.github.io/2017/03/28/go-create-directory-if-not-exist/
func createDirIfNotExist(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			panic(err)
		}
	}
}

func main() {

	// Import backlog from JIRA

	backlogMap := make(map[string]backlogItem)

	// Read from stdio treating it as a csv
	r := csv.NewReader(bufio.NewReader(os.Stdin))
	r.LazyQuotes = true

	// Parse into a map of stories
	firstLine := true
	for {
		records, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		// Dynamically determine the position in the CSV record of the fields we need
		if firstLine {
			firstLine = false
			columnIndexMap := make(map[string]int)
			for i, val := range records {
				columnIndexMap[val] = i
			}
			ndxIssueID = columnIndexMap[fieldIssueID]
			ndxIssueKey = columnIndexMap[fieldIssueKey]
			ndxIssueType = columnIndexMap[fieldIssueType]
			ndxStatus = columnIndexMap[fieldStatus]
			ndxCreated = columnIndexMap[fieldCreated]
			ndxResolved = columnIndexMap[fieldResolved]
			ndxLabels = columnIndexMap[fieldLabels]
			ndxPoints = columnIndexMap[fieldPoints]
			ndxParentKey = columnIndexMap[fieldParentKey]
			continue
		}

		// See if the backlog item already exists
		existingItem, ok := backlogMap[records[ndxIssueKey]]

		// If backlog item already exists but indicates that it has no children then we know we are encountering
		// a duplicate record which we will ignore
		if ok && !existingItem.hasChildren {
			log.Printf("WARNING: Encountered an unexpected duplicate item: \"%s\"", records[ndxIssueID])
			continue
		}

		// Transformations
		var points float64
		var opened time.Time
		var closed time.Time
		if records[ndxPoints] != "" {
			points, err = strconv.ParseFloat(records[ndxPoints], 64)
			if err != nil {
				log.Printf("WARNING: Unable to convert %s's story points of \"%s\" to an integer", records[ndxIssueID], records[ndxPoints])
			}
		}
		if records[ndxCreated] != "" {
			opened, err = time.Parse(jiraDate, records[ndxCreated])
			if err != nil {
				log.Printf("WARNING: Unable to reformat %s's creation date of \"%s\"", records[ndxIssueID], records[ndxPoints])
			}
		}
		if records[ndxResolved] != "" {
			closed, err = time.Parse(jiraDate, records[ndxResolved])
			if err != nil {
				log.Printf("WARNING: Unable to reformat %s's resolution date of \"%s\"", records[ndxIssueID], records[ndxPoints])
			}
		}

		// Having dealt with an unexpected duplicate record above, if the backlog item already exists at this
		// point then it was a placeholder created when we encountered the child before the parent.  In this case,
		// we will update everything preserving the hasChildren value and ignoring its story points.  Otherwise, we
		// will add the completley new item to the map
		if ok {
			backlogMap[records[ndxIssueKey]] = backlogItem{
				itemType:    records[ndxIssueType],
				id:          records[ndxIssueID],
				parent:      records[ndxParentKey],
				hasChildren: true,
				opened:      opened,
				closed:      closed,
				tags:        records[ndxLabels],
			}
		} else {
			backlogMap[records[ndxIssueKey]] = backlogItem{
				itemType:    records[ndxIssueType],
				id:          records[ndxIssueID],
				parent:      records[ndxParentKey],
				hasChildren: false,
				opened:      opened,
				closed:      closed,
				points:      points,
				tags:        records[ndxLabels],
			}
		}

		// Zero out any parent points
		parentKey := records[ndxParentKey]
	parentWalk:
		for parentKey != "" {

			parentItem, ok := backlogMap[parentKey]

			// We have seen a child before we've seen the parent, so add a placeholder
			// and move on
			if !ok {
				backlogMap[parentKey] = backlogItem{
					hasChildren: true,
				}
				break parentWalk
			}

			// We have a parent so make sure its story points are zero and that the
			// indicator that it has children is set
			parentItem.hasChildren = true
			parentItem.points = 0
			backlogMap[parentKey] = parentItem

			// And walk up the chain to its parent if one exists
			parentKey = parentItem.parent
		}
	}

	// list only the leaf items
	var backlog strings.Builder
	fmt.Fprintf(&backlog, "\"%s\",\"%s\",\"%s\",\"%s\",\"%s\"\n", "type", "id", "opened", "closed", "points")
	totalPoints := 0.0
	for _, item := range backlogMap {
		if item.hasChildren {
			continue
		}
		totalPoints += item.points
		fmt.Fprintf(&backlog, "\"%s\",", item.itemType)
		fmt.Fprintf(&backlog, "\"%s\",", item.id)
		fmt.Fprintf(&backlog, "\"%s\",", item.opened.Format(isoDate))
		if item.closed.Equal(time.Time{}) {
			fmt.Fprintf(&backlog, "\"\",")
		} else {
			fmt.Fprintf(&backlog, "\"%s\",", item.closed.Format(isoDate))
		}
		fmt.Fprintf(&backlog, "%.2f", item.points)
		fmt.Fprintf(&backlog, "\n")
	}
	createDirIfNotExist("Burnup/Snapshots")
	err := ioutil.WriteFile(fmt.Sprintf("Burnup/Snapshots/%s %s.%s", "Backlog Snapshot", time.Now().Format(isoDate), "csv"), []byte(backlog.String()), 0644)
	if err != nil {
		log.Fatalf("FATAL: Unable to write file to disk: %s\n", err)
	}

	// list items missing points
	var noPoints strings.Builder
	fmt.Fprintf(&noPoints, "\"%s\",\"%s\",\"%s\"\n", "type", "id", "closed")
	for _, item := range backlogMap {
		if item.hasChildren {
			continue
		}
		if item.points != 0 {
			continue
		}
		fmt.Fprintf(&noPoints, "\"%s\",\"%s\",%t\n", item.itemType, item.id, !item.closed.Equal(time.Time{}))
	}
	createDirIfNotExist("Burnup/Audits")
	err = ioutil.WriteFile(fmt.Sprintf("Burnup/Audits/%s %s.%s", "No Points", time.Now().Format(isoDate), "csv"), []byte(noPoints.String()), 0644)
	if err != nil {
		log.Fatalf("FATAL: Unable to write file to disk: %s\n", err)
	}

	// Aggregate the backlog by date
	type openPivotStruct struct {
		date   time.Time
		points float64
	}

	type closedPivotStruct struct {
		date   time.Time
		points float64
	}

	openPivot := make(map[string]openPivotStruct)
	closedPivot := make(map[string]closedPivotStruct)
	firstDate := time.Time{}
	lastDate := time.Time{}

	for _, item := range backlogMap {

		// Skip any items with no points
		if item.points > 0.0 {

			// Accumulate points opened on each day
			openValue, _ := openPivot[item.opened.Format(isoDate)]
			openValue.date = item.opened
			openValue.points += item.points
			openPivot[item.opened.Format(isoDate)] = openValue
			if firstDate.Equal(time.Time{}) || firstDate.After(item.opened) {
				firstDate = item.opened
			}
			if lastDate.Equal(time.Time{}) || lastDate.Before(item.opened) {
				lastDate = item.opened
			}

			// Accumulate points closed on each day
			if !item.closed.Equal(time.Time{}) {
				closedValue, _ := closedPivot[item.closed.Format(isoDate)]
				closedValue.date = item.closed
				closedValue.points += item.points
				closedPivot[item.closed.Format(isoDate)] = closedValue
				if firstDate.Equal(time.Time{}) || firstDate.After(item.closed) {
					firstDate = item.closed
				}
				if lastDate.Equal(time.Time{}) || lastDate.Before(item.closed) {
					lastDate = item.closed
				}
			}
		}
	}

	// Generate running totals table
	var snapshot strings.Builder
	fmt.Fprintf(&snapshot, "\"%s\",\"%s\",\"%s\"\n", "date", "pointsOpened", "pointsClosed")
	for date := firstDate; date.Before(lastDate); date = date.AddDate(0, 0, 1) {
		pointsOpened := openPivot[date.Format(isoDate)].points
		pointsClosed := closedPivot[date.Format(isoDate)].points
		fmt.Fprintf(&snapshot, "%s,%.2f,%.2f\n", date.Format(isoDate), pointsOpened, pointsClosed)
	}
	createDirIfNotExist("Burnup/Totals")
	err = ioutil.WriteFile(fmt.Sprintf("Burnup/Totals/%s %s.%s", "Totals", time.Now().Format(isoDate), "csv"), []byte(snapshot.String()), 0644)
	if err != nil {
		log.Fatalf("FATAL: Unable to write file to disk: %s\n", err)
	}
}
