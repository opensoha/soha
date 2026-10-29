CREATE TABLE public.content_transfer_destinations (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, revision BIGINT NOT NULL CHECK(revision>0),
 draft JSONB NOT NULL, published JSONB, published_version BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL,
 CHECK((published IS NULL AND published_version=0) OR (published IS NOT NULL AND published_version>0 AND published_version<=revision))
);
CREATE TABLE public.content_transfer_records (
 id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,destination_id TEXT NOT NULL REFERENCES public.content_transfer_destinations(id),
 source_id TEXT NOT NULL DEFAULT '',action TEXT NOT NULL CHECK(action IN ('upload','paste','copy','download','delete')),
 state TEXT NOT NULL CHECK(state IN ('prepared','sending','blocked','completed','failed','unknown')),
 revision BIGINT NOT NULL CHECK(revision>0),metadata JSONB NOT NULL,private_metadata JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,updated_at TIMESTAMPTZ NOT NULL,deleted_at TIMESTAMPTZ
);
CREATE INDEX content_transfer_records_owner ON public.content_transfer_records(actor_id,created_at DESC,id DESC);
CREATE INDEX content_transfer_records_destination ON public.content_transfer_records(destination_id,created_at DESC,id DESC);
CREATE TABLE public.content_transfer_audit (
 id BIGSERIAL PRIMARY KEY,actor_id TEXT NOT NULL,destination_id TEXT NOT NULL,record_id TEXT NOT NULL DEFAULT '',
 action TEXT NOT NULL,metadata JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX content_transfer_audit_destination ON public.content_transfer_audit(destination_id,id DESC);
