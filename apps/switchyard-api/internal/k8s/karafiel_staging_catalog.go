package k8s

// Catalog descriptors only, never application/migration/user rows. Both the
// fixed migrator and nonowner readiness reader hash the exact PostgreSQL text.
const KarafielSchemaCatalogSQL = `SELECT jsonb_build_array(
(SELECT coalesce(jsonb_agg(jsonb_build_array(c.relname,c.relkind,c.relrowsecurity,c.relforcerowsecurity) ORDER BY c.relname),'[]'::jsonb) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','S')),
(SELECT coalesce(jsonb_agg(jsonb_build_array(c.relname,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attidentity,a.attgenerated,pg_get_expr(d.adbin,d.adrelid)) ORDER BY c.relname,a.attnum),'[]'::jsonb) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND a.attnum>0 AND NOT a.attisdropped),
(SELECT coalesce(jsonb_agg(jsonb_build_array(c.relname,k.conname,k.contype,pg_get_constraintdef(k.oid,true)) ORDER BY c.relname,k.conname),'[]'::jsonb) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
(SELECT coalesce(jsonb_agg(jsonb_build_array(c.relname,i.relname,pg_get_indexdef(i.oid)) ORDER BY c.relname,i.relname),'[]'::jsonb) FROM pg_index x JOIN pg_class c ON c.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public')
)::text`
