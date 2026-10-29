SELECT pg_catalog.set_config('search_path', '', false);
CREATE TABLE public.network_proxy_configuration_history (
 tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
 instance_id text NOT NULL REFERENCES public.network_proxy_instances(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0), content_encrypted text NOT NULL, content_hash text NOT NULL,
 created_at timestamptz NOT NULL, PRIMARY KEY(tenant_id,workspace_id,instance_id,revision)
);
INSERT INTO public.network_proxy_configuration_history(tenant_id,workspace_id,instance_id,revision,content_encrypted,content_hash,created_at)
 SELECT tenant_id,workspace_id,id,desired_revision,desired_content_encrypted,desired_hash,updated_at FROM public.network_proxy_instances WHERE desired_revision>0;
