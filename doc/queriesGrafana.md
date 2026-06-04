### Grafana query
```
http://prometheus:9090
```

```
sum by (pod_name , status) (queue_tasks_processed_total{status="success"})
```