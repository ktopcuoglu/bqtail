package status

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"google.golang.org/api/bigquery/v2"
	"testing"
)

func TestURIs_Classify(t *testing.T) {
	var useCases = []struct {
		description         string
		job                 string
		expectFields        []*Field
		expectMissing       []string
		expectCorrupted     []string
		expectInvalidSchema []string
		expectedValid       []string
	}{
		{
			description:         "missing file in gs",
			expectCorrupted:     []string{},
			expectInvalidSchema: []string{},
			expectMissing:       []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro"},
			expectedValid:       []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceFormat": "AVRO",
      "sourceUris": [
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"
      ],
      "useAvroLogicalTypes": true,
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
    "errorResult": {
      "message": "Not found: URI gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
      "reason": "notFound"
    },
    "errors": [
      {
        "message": "Not found: URI gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
        "reason": "notFound"
      }
    ],
    "state": "DONE"
  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},

		{
			description:         "missing file in bigstore",
			expectCorrupted:     []string{},
			expectInvalidSchema: []string{},
			expectMissing:       []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro"},
			expectedValid:       []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceFormat": "AVRO",
      "sourceUris": [
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"
      ],
      "useAvroLogicalTypes": true,
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
    "errorResult": {
      "message": "Not found: URI gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
      "reason": "notFound"
    },
    "errors": [
      {
        "message": "Not found: Files /bigstore/mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
        "reason": "notFound"
      }
    ],
    "state": "DONE"
  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},

		{
			description:         "corrupted file",
			expectMissing:       []string{},
			expectInvalidSchema: []string{},
			expectCorrupted:     []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro"},
			expectedValid:       []string{"gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceFormat": "AVRO",
      "sourceUris": [
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro",
        "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log.2019-11-19_19-41.1.i-03d29a135680c7b13.gz-v0.avro"
      ],
      "useAvroLogicalTypes": true,
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
    "errorResult": {
      "message": "Invalid JSON payload received. Unexpected token.",
      "reason": "invalid"
    },
    "errors": [
      {
        "message": "Invalid JSON payload received. Unexpected token.",
        "reason": "invalid",
		"location": "gs://mybucket/nobid/xlog.request/2019/11/19/19/xlog.request.log-3.2019-11-19_19-33.1.i-0c50bdd516f3eb445.gz-v0.avro"
      }
    ],
    "state": "DONE"
  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},

		{
			description:         "invalid schema",
			expectMissing:       []string{},
			expectCorrupted:     []string{},
			expectInvalidSchema: []string{"gs://myproject_bqtail/data/case018/dummy2.json"},
			expectedValid:       []string{"gs://myproject_bqtail/data/case018/dummy.json"},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceFormat": "NEWLINE_DELIMITED_JSON",
      "sourceUris": [
         "gs://myproject_bqtail/data/case018/dummy.json",
         "gs://myproject_bqtail/data/case018/dummy2.json"
      ],
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
    "errorResult": {
      "location": "gs://myproject_bqtail/data/case018/dummy2.json",
      "message": "Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 2; errors: 1. Please look into the errors[] collection for more details.",
      "reason": "invalid"
    },
    "errors": [
      {
        "location": "gs://myproject_bqtail/data/case018/dummy2.json",
        "message": "Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 2; errors: 1. Please look into the errors[] collection for more details.",
        "reason": "invalid"
      },
      {
        "message": "Error while reading data, error message: JSON processing encountered too many errors, giving up. Rows: 2; errors: 1; max bad: 0; error percent: 0",
        "reason": "invalid"
      },
      {
        "location": "gs://myproject_bqtail/data/case018/dummy2.json",
        "message": "Error while reading data, error message: JSON parsing error in row starting at position 43: Could not convert value to string. Field: name; Value: 3",
        "reason": "invalid"
      }
    ],
    "state": "DONE"
  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},

		{
			description:         "corrupted JSON data",
			expectMissing:       []string{},
			expectCorrupted:     []string{"gs://myproject_bqtail/data/case021/dummy2.json"},
			expectInvalidSchema: []string{},
			expectedValid:       []string{},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceFormat": "NEWLINE_DELIMITED_JSON",
      "sourceUris": [
         "gs://myproject_bqtail/data/case021/dummy2.json"
      ],
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
    "errorResult": {
      "location": "gs://myproject_bqtail/data/case021/dummy2.json",
      "message": "Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 12; errors: 1. Please look into the errors[] collection for more details.",
      "reason": "invalid"
    },
    "errors": [
  {
    "location": "gs://myproject_bqtail/data/case021/dummy2.json",
    "message": "Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 12; errors: 1. Please look into the errors[] collection for more details.",
    "reason": "invalid"
  },
  {
    "message": "Error while reading data, error message: JSON processing encountered too many errors, giving up. Rows: 12; errors: 1; max bad: 0; error percent: 0",
    "reason": "invalid"
  },
  {
    "location": "gs://myproject_bqtail/data/case021/dummy2.json",
    "message": "Error while reading data, error message: JSON parsing error in row starting at position 497: Closing quote expected in string",
    "reason": "invalid"
  }
    ],
    "state": "DONE"
  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},
		{
			description:         "missing schema field",
			expectCorrupted:     []string{},
			expectInvalidSchema: []string{"gs://viant_e2e_bqtail/data/case038/path2/dummy2.json"},
			expectMissing:       []string{},
			expectedValid:       []string{"gs://viant_e2e_bqtail/data/case038/path2/dummy1.json"},
			expectFields: []*Field{
				{
					Row:      1,
					Name:     "name",
					Location: "gs://viant_e2e_bqtail/data/case038/path2/dummy2.json",
				},
			},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceUris": [
        "gs://viant_e2e_bqtail/data/case038/path2/dummy1.json",
        "gs://viant_e2e_bqtail/data/case038/path2/dummy2.json"
      ],
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
  "status": {
	"errorResult":{"location":"gs://viant_e2e_bqtail/data/case038/path2/dummy2.json","message":"Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 1; errors: 1. Please look into the errors[] collection for more details.",
	"reason":"invalid"},
	"errors":[{"Location":"gs://viant_e2e_bqtail/data/case038/path2/dummy2.json","Message":"Error while reading data, error message: JSON table encountered too many errors, giving up. Rows: 1; errors: 1. Please look into the errors[] collection for more details.","Reason":"invalid"},
             {"Message":"Error while reading data, error message: JSON processing encountered too many errors, giving up. Rows: 1; errors: 1; max bad: 0; error percent: 0","Reason":"invalid"},
			 {"Location":"gs://viant_e2e_bqtail/data/case038/path2/dummy2.json","Message":"Error while reading data, error message: JSON parsing error in row starting at position 0: No such field: name.","Reason":"invalid"}],
	"state":"DONE"

  },
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},

		{
			description:         "missing bigstore file",
			expectCorrupted:     []string{},
			expectInvalidSchema: []string{},
			expectMissing:       []string{"gs://viant_e2e_bqtail/data/case038/path2/dummy2.json"},
			expectedValid:       []string{"gs://viant_e2e_bqtail/data/case038/path2/dummy1.json"},
			expectFields:        []*Field{},
			job: `{
  "configuration": {
    "jobType": "LOAD",
    "load": {
      "createDisposition": "CREATE_IF_NEEDED",
      "destinationTable": {
        "datasetId": "temp",
        "projectId": "myproject",
        "tableId": "mytable"
      },
      "sourceUris": [
        "gs://viant_e2e_bqtail/data/case038/path2/dummy1.json",
        "gs://viant_e2e_bqtail/data/case038/path2/dummy2.json"
      ],
      "writeDisposition": "WRITE_TRUNCATE"
    }
  },
  "etag": "CPmxTyCVv2jOT55WwdVweg==",
  "id": "myproject:US.temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
  "jobReference": {
    "jobId": "temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch",
    "location": "US",
    "projectId": "myproject"
  },
  "kind": "bigquery#jobID",
  "selfLink": "https://www.googleapis.com/bigquery/v2/projects/myproject/jobs/temp--x_zzz_39_20191119_439770381788305--439770381788305--dispatch?location=US",
  "statistics": {
    "creationTime": "1574193994917",
    "endTime": "1574193995142",
    "startTime": "1574193995061"
  },
 "status": {
				"errorResult": {
					"message": "Not found: Files /bigstore/viant_e2e_bqtail/data/case038/path2/dummy2.json",
					"reason": "notFound"
				},
				"errors": [
					{
						"message": "Not found: Files /bigstore/viant_e2e_bqtail/data/case038/path2/dummy2.json",
						"reason": "notFound"
					}
				],
				"state": "DONE"
			},
  "user_email": "myproject-cloud-function@myproject.iam.gserviceaccount.com"
}`,
		},
	}
	for _, useCase := range useCases {
		job := &bigquery.Job{}
		err := json.Unmarshal([]byte(useCase.job), &job)
		if !assert.Nil(t, err, useCase.description) {
			continue
		}

		assert.Nil(t, err, useCase.description)
		uris := NewURIs()
		uris.Classify(context.Background(), nil, job)

		assert.EqualValues(t, useCase.expectMissing, uris.Missing, useCase.description)
		assert.EqualValues(t, useCase.expectCorrupted, uris.Corrupted, useCase.description)
		assert.EqualValues(t, useCase.expectInvalidSchema, uris.InvalidSchema, useCase.description)
		assert.EqualValues(t, useCase.expectedValid, uris.Valid, useCase.description)
		if len(useCase.expectFields) > 0 {
			assert.EqualValues(t, useCase.expectFields, uris.MissingFields, useCase.description)

		}
	}

}
