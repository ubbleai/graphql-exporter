## GrapqhQL exporter

Exporter is designed to build and display metrics based on GraphQL query results.

`config_examle.json` shows basic usage case (in this case data from [Netbox](https://docs.netbox.dev)) and builds 3 metrics from PromQL query result given below:


```{
  "data": {
    "device_list": [
      {
        "name": "server",
        "serial": "1234",
        "custom_fields": {
          "price": "7408.80",
          "mgmt_mac": "11:11:11:11:11:00",
          "cpu_count": null,
          "mgmt_user": "root",
          "order_date": "2022-03-01",
          "memory_total": 512,
          "mgmt_password": "password",
          "price_per_month": "205.8",
          "depreciation_date": "2025-02-28",
          "depreciation_rate": "33.33",
          "order_contract_id": "contract-nr1",
          "storage_total_hdd": 0,
          "storage_total_ssd": 893
        }
      }
    ]
  }
}
```

Query supports dynamic `datetime` field. It can be specified as template variable.
For example when specified `{{NOW \"-1h\"}}` query will generate `datetime` that existed one hour ago.

Metrics results in:

```
# HELP graphql_exporter_custom_fields_depreciation_date Deprecation date
# TYPE graphql_exporter_custom_fields_depreciation_date gauge
graphql_exporter_custom_fields_depreciation_date{name="server",order_contract_id="contract-nr1",serial="1234",value="2025-02-28"} 1
# HELP graphql_exporter_custom_fields_memory_total Device memory total
# TYPE graphql_exporter_custom_fields_memory_total gauge
graphql_exporter_custom_fields_memory_total{name="server",order_contract_id="contract-nr1"} 512
# HELP graphql_exporter_custom_fields_price Device price
# TYPE graphql_exporter_custom_fields_price gauge
graphql_exporter_custom_fields_price{name="server",order_contract_id="contract-nr1"} 7408.8
```

API token can be overridden with `GRAPHQLAPITOKEN` env variable.
`CacheExpire` configuration parameter defines cache validity period. Value of `0` disables caching.

## Dev env
### from Dockerfile
Build the image
```
docker buildx build --platform linux/amd64,linux/arm64 --push -t registry.ubble.ai/ubbleai/sandbox/graphql-exporter:dev -f Dockerfile ./
```
Run the container
```
docker run -it -v ./gitlab.yaml:/gitlab-yaml --rm -p 9353:9353 registry.ubble.ai/ubbleai/sandbox/graphql-exporter:dev
```

## Unused label eviction

GraphQL responses with high-cardinality fields (e.g. GitLab `pipeline_ref`, `job_name`) cause the in-process Prometheus vecs to accumulate label combinations that never reappear (deleted MRs, retired jobs). Set `unusedLabelTTLSeconds` to evict a label child once it has not been seen by a successful scrape for that duration.

```yaml
unusedLabelTTLSeconds: 21600   # 6h; 0 (default) disables eviction
```

Behaviour:

- Eviction runs at the end of each **successful** scrape (skipped if the underlying GraphQL query errored — no purge on transient outages).
- Per-child: only stale series are removed. Active counters keep their accumulated value across scrapes; only series that have not been seen for `unusedLabelTTLSeconds` are dropped.
- Counter resets: when an evicted label combination reappears, its counter restarts at zero. PromQL `rate()` / `increase()` handle counter resets natively.
- Zero-dimension metrics (no labels) are not tracked — there is at most one child.

The exporter exposes two observability metrics under the `ubbleai_graphql_exporter_exporter_` prefix:

| Metric | Type | Meaning |
|--------|------|---------|
| `ubbleai_graphql_exporter_exporter_evicted_labels_total{metric}` | counter | label combinations removed by the TTL policy |
| `ubbleai_graphql_exporter_exporter_tracked_labels{metric}` | gauge | current size of the label-last-seen map (steady-state cardinality) |

When `unusedLabelTTLSeconds=0` the two metrics emit no series (the eviction code never runs).
