SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_mihomo_profiles
  ADD COLUMN source_type TEXT,
  ADD COLUMN manual_node_ciphertext TEXT;

UPDATE public.network_mihomo_profiles
SET source_type = 'managed_subscription'
WHERE mode = 'managed_follow';

ALTER TABLE public.network_mihomo_profiles
  DROP CONSTRAINT network_mihomo_profiles_check2,
  ADD CONSTRAINT network_mihomo_profiles_source_type_check
    CHECK (source_type IS NULL OR source_type IN ('managed_subscription', 'manual_node')),
  ADD CONSTRAINT network_mihomo_profiles_source_check CHECK (
    (mode = 'managed_follow' AND source_type = 'managed_subscription' AND subscription_url_ciphertext IS NOT NULL AND manual_node_ciphertext IS NULL AND selected_proxy IS NOT NULL AND fail_closed)
    OR
    (mode = 'managed_follow' AND source_type = 'manual_node' AND subscription_url_ciphertext IS NULL AND manual_node_ciphertext IS NOT NULL AND selected_proxy IS NULL AND fail_closed)
    OR
    (mode = 'app_subscription' AND source_type IS NULL AND subscription_url_ciphertext IS NULL AND manual_node_ciphertext IS NULL AND selected_proxy IS NULL)
  );
