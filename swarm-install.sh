docker build -t rest ./rest
docker build -t worker ./worker
docker build -t grafana ./grafana
docker build -t prometheus ./prometheus

docker stack deploy -c .\docker-swarm.yml sync-to-async --detach=false