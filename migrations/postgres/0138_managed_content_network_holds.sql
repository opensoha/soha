-- Preserve Docker source FKs and deletion protection; managed content has its own real source.
ALTER TABLE public.network_security_holds ALTER COLUMN source_event_id DROP NOT NULL;
ALTER TABLE public.network_security_holds ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE public.network_security_holds ADD COLUMN source_transfer_id TEXT UNIQUE REFERENCES public.content_transfer_records(id);
ALTER TABLE public.network_security_holds ADD COLUMN destination_id TEXT REFERENCES public.content_transfer_destinations(id);
ALTER TABLE public.network_security_holds DROP CONSTRAINT network_security_holds_source_type_check;
ALTER TABLE public.network_security_holds ADD CONSTRAINT network_security_holds_source_type_check CHECK(source_type IN ('content_dlp','managed_content'));
ALTER TABLE public.network_security_holds ADD CONSTRAINT network_security_holds_source_identity CHECK(
 (source_type='content_dlp' AND source_event_id IS NOT NULL AND project_id IS NOT NULL AND source_transfer_id IS NULL AND destination_id IS NULL) OR
 (source_type='managed_content' AND source_transfer_id IS NOT NULL AND destination_id IS NOT NULL AND source_event_id IS NULL AND project_id IS NULL));
CREATE UNIQUE INDEX network_security_hold_active_destination ON public.network_security_holds(subject_id,destination_id) WHERE active AND source_type='managed_content';
CREATE INDEX content_transfer_record_network_hold ON public.content_transfer_records((metadata->>'networkHoldId')) WHERE metadata ? 'networkHoldId';
