### Data transformation 

### Scenario:

This scenario test a transformation expressions defined below:

[@rule.yaml](rule/rule.yaml)
```yaml
When:
  Prefix: /data/case${parentIndex}/
  Suffix: .json
Dest:
  Table: bqtail.dummy_v${parentIndex}
  Transient:
    Dataset: temp
  Transform:
    event_type: CASE WHEN type_id =1 THEN 'type 1' WHEN type_id = 2 THEN 'type 2'  WHEN
      type_id = 3 THEN 'type 3' END
Async: true
OnSuccess:
  - Action: delete

```
