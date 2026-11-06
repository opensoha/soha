SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_gateways DROP CONSTRAINT network_access_gateways_overlay_cidr_check;
ALTER TABLE public.network_access_gateways ADD CONSTRAINT network_access_gateways_overlay_cidr_check
 CHECK(overlay_cidr IS NULL OR
 (family(overlay_cidr)=4 AND masklen(overlay_cidr) BETWEEN 16 AND 30) OR
 (family(overlay_cidr)=6 AND masklen(overlay_cidr) BETWEEN 64 AND 126 AND routing_mode='routed'));
ALTER TABLE public.network_wireguard_peers DROP CONSTRAINT network_wireguard_peers_overlay_address_check;
ALTER TABLE public.network_wireguard_peers ADD CONSTRAINT network_wireguard_peers_overlay_address_check
 CHECK((family(overlay_address)=4 AND masklen(overlay_address)=32) OR (family(overlay_address)=6 AND masklen(overlay_address)=128));
