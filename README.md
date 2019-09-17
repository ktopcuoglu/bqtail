# BigQuery Tail (bqtail)

This library is compatible with Go 1.11+

Please refer to [`CHANGELOG.md`](CHANGELOG.md) if you encounter breaking changes.

- [Motivation](#motivation)
- [Introduction](#introduction)
- [Tail Service](tail/README.md)
- [Dispatch Service](dispatch/README.md)
- [Usage](#usage)
- [End to end testing](#end-to-end-testing)

## Motivation

The goal of this project is to provide cost effective events driven, data ingestion and extraction.

## Introduction


![BqTail](images/bqtail.png)


- [Tail Service](tail/README.md)
- [Dispatch Service](dispatch/README.md)
- [Task Service](task/README.md)


## Usage


- **Data ingestion**

The following define configuration to ingest data in batches within 30 sec time window in async mode.

[@config/bqtail.json](usage/batch/tail.json)
```json
{
  "BatchURL": "gs://myBucket/batch/",
  "ErrorURL": "gs://myBucket/errors/",
  "JournalURL": "gs://myBucket/journal/",
  "DeferTaskURL": "gs://myBucket/tasks/",
  "Routes": [
    {
      "When": {
        "Prefix": "/data/",
        "Suffix": ".avro"
      },
      "Dest": {
        "Table": "mydataset.mytable"
      },
      "Async": true,
      "Batch": {
        "Window": {
          "DurationInSec": 15
        }
      },
      "OnSuccess": [
        {
          "Action": "delete"
        }
      ],
      "OnFailure": [
        {
          "Action": "move",
          "Request": {
            "DestURL": "gs://myBucket/errors"
          }
        },
        {
          "Action": "notify",
          "Request": {
            "Channels": [
              "#e2e"
            ],
            "From": "BqTail",
            "Title": "bqtail.wrong_dummy ingestion",
            "Message": "$Error",
            "Secret": {
              "URL": "gs://${config.Bucket}/config/slack.json.enc",
              "Key": "bqtail_ring/bqtail_key"
            }
          }
        }
      ]
    }
  ]
}
```
[@config/dispatch.json](usage/batch/dispatch.json)
```json
{
  "JournalURL": "gs://myBucket/journal/",
  "ErrorURL": "gs://myBucket/errors/",
  "DeferTaskURL": "gs://myBucket/tasks/"
}
``` 

- **Data ingestion with deduplication**

The following define configuration to ingest data in batches within 30 sec time window in async mode.

[@config/bqtail.json](usage/dedupe/tail.json)
```json
{
  "BatchURL": "gs://myBucket/batch/",
  "ErrorURL": "gs://myBucket/errors/",
  "JournalURL": "gs://myBucket/journal/",
  "DeferTaskURL": "gs://myBucket/tasks/",
  "Routes": [
    {
      "Async": true,
      "When": {
        "Prefix": "/data/",
        "Suffix": ".avro"
      },
      "Dest": {
        "Table": "mydataset.mytable",
        "TempDataset": "transfer",
        "UniqueColumns": ["id"]
      },
      "Batch": {
        "Window": {
          "DurationInSec": 60
        }
      },
      "OnSuccess": [
        {
          "Action": "query",
          "Request": {
            "SQL": "SELECT '$JobID' AS job_id, COUNT(1) AS row_count, CURRENT_TIMESTAMP() AS completed FROM $DestTable",
            "Dest": "mydataset.summary"
          }
        },
        {
          "Action": "delete"
        }
      ],
      "OnFailure": [
        {
          "Action": "move",
          "Request": {
            "DestURL": "gs://myBucket/errors"
          }
        },
        {
          "Action": "notify",
          "Request": {
            "Channels": [
              "#e2e"
            ],
            "From": "BqTail",
            "Title": "bqtail.wrong_dummy ingestion",
            "Message": "$Error",
            "Secret": {
              "URL": "gs://${config.Bucket}/config/slack.json.enc",
              "Key": "bqtail_ring/bqtail_key"
            }
          }
        }
      ]
    }
  ]
}
```




- **Data extraction**

The following define configuration to extract data to google storate after target table is modified.

```json
{
   "JournalURL": "gs://myBucket/journal/",
   "ErrorURL": "gs://myBucket/errors/",
   "Routes": [
     {
       "When": {
         "Dest": ".+:mydataset\\.mytable",
         "Type": "QUERY"
       },
       "OnSuccess": [
         {
           "Action": "export",
           "Request": {
             "DestURL": "gs://${config.Bucket}/export/mytable.json.gz"
           }
         }
       ]
     }
   ]
 }
```

## Deployment

**Prerequisites**

The following URL are used by tail/dispatch services:

- JournalURL - job history journal 
- ErrorURL - job that resulted in an error
- DeferTaskURL - transient storage for managing deferred tasks (tail in async mode). 
- BatchURL - transient storage for managing event batching.

**Cloud function deployments**

- [BqTail](tail/README.md#deployment)
- [BqDispatch](dispatch/README.md#deployment)




With [endly](https://github.com/viant/endly/) automation runner

```bash

endly deploy.yaml

```

Where: [@deploy.yaml](deployment/deploy.yaml) 


```yaml
init:
  appPath: $WorkingDirectory(..)
  target:
    URL: ssh://127.0.0.1/
    credentials: localhost
  gcpSecrets: gcp-e2e
  gcp: ${secrets.$gcpSecrets}
  projectID: $gcp.ProjectID
  serviceAccount: $gcp.ClientEmail

defaults:
  credentials: $gcpSecrets
pipeline:

    package:
      action: exec:run
      comments: vendor build for deployment speedup
      target: $target
      checkError: true
      commands:
        - export GIT_TERMINAL_PROMPT=1
        - export GO111MODULE=on
        - unset GOPATH
        - cd ${appPath}/
        - go mod vendor
        - go build

    uploadConfig:
      tail:
        action: storage:copy
        source:
          URL: config/tail.json
        dest:
          URL: gs://myconfigbucket/config/tail.json
          credentials: $gcpSecrets
      dispatch:
        action: storage:copy
        source:
          URL: config/dispatch.json
        dest:
          URL: gs://myconfigbucket/config/dispatch.json
          credentials: $gcpSecrets
        
    deploy:
      tail:
        action: gcp/cloudfunctions:deploy
        '@name': MyDataBucketBqTail
        timeout: 540s
        public: true
        availableMemoryMb: 256
        entryPoint: BqTail
        runtime: go111
        environmentVariables:
          CONFIG: gs://myconfigbucket/config/tail.json
        eventTrigger:
          eventType: google.storage.object.finalize
          resource: projects/_/buckets/mydatabucket
        source:
          URL: ${appPath}/


      dispach:
        action: gcp/cloudfunctions:deploy
        '@name': BqDispatch
        timeout: 540s
        public: true
        availableMemoryMb: 256
        entryPoint: BqDispatch
        runtime: go111
        environmentVariables:
          CONFIG: gs://myconfigbucket/config/dispatch.json
        eventTrigger:
          eventType: google.cloud.bigquery.job.complete
          resource: projects/${projectID}/jobs/{jobId}
        source:
          URL: ${appPath}/

```



### Monitoring

- Check for any files under ErrorURL
- DeferTaskURL should not have very old files, unless there is processsing error
- BatchURL should not have very old files, unless there is processing error


## End to end testing

You can try on all data ingestion and extraction scenarios by simply running e2e test cases:

- [Prerequisites](e2e/README.md#prerequisites)
- [Use cases](e2e/README.md#use-cases)

## License

The source code is made available under the terms of the Apache License, Version 2, as stated in the file `LICENSE`.

Individual files may be made available under their own specific license,
all compatible with Apache License, Version 2. Please see individual files for details.

<a name="Credits-and-Acknowledgements"></a>

## Credits and Acknowledgements

**Library Author:** Adrian Witas

