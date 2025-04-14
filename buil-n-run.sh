docker build -t rest ./rest
docker build -t worker ./worker
docker build -t prometheus ./prometheus
docker build -t grafana ./grafana

docker swarm init
docker stack rm sync-to-async
docker stack deploy -c .\docker-swarm.yml sync-to-async