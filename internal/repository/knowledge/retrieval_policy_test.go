package knowledge

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestListRetrievalPoliciesScopesSelectedActiveBasesInOneQuery(t *testing.T) {
	repo, mock := newKnowledgeRepository(t)
	query := `SELECT b.id,b.retrieval_policy FROM ai_knowledge_bases b WHERE b.status='active' AND b.deleted_at IS NULL AND (b.owner_id = $1 OR b.scope->>'visibility' = 'public' OR jsonb_exists(b.scope->'users', $2)) AND b.id IN (SELECT jsonb_array_elements_text($3::jsonb))`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("reader", "reader", `["a","hidden"]`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "retrieval_policy"}).AddRow("a", `{"defaultTopK":3,"maxTopK":8,"lexicalWeight":1}`)).RowsWillBeClosed()
	policies, err := repo.ListRetrievalPolicies(t.Context(), domainknowledge.PrincipalScope{UserID: "reader"}, []string{"a", "hidden"})
	if err != nil || len(policies) != 1 || policies["a"].DefaultTopK != 3 || policies["a"].LexicalWeight != 1 {
		t.Fatalf("scoped policies = %+v, %v", policies, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListRetrievalPoliciesReturnsReadError(t *testing.T) {
	repo, mock := newKnowledgeRepository(t)
	want := errors.New("policy query failed")
	mock.ExpectQuery(`SELECT b.id,b.retrieval_policy FROM ai_knowledge_bases`).WillReturnError(want)
	if _, err := repo.ListRetrievalPolicies(t.Context(), domainknowledge.PrincipalScope{}, []string{"a"}); !errors.Is(err, want) {
		t.Fatalf("policy read error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRetrievalPolicyValuesUsesOneJSONBind(t *testing.T) {
	query, args, err := retrievalPolicyValues(map[string]domainknowledge.RetrievalPolicy{"a'": {LexicalWeight: .5, VectorWeight: .5}})
	if err != nil || query != `SELECT * FROM jsonb_to_recordset(?::jsonb) AS p(knowledge_base_id text,lexical_weight float8,vector_weight float8,min_score float8)` || len(args) != 1 {
		t.Fatalf("policy values = %q, %+v, %v", query, args, err)
	}
	data, ok := args[0].(string)
	if !ok {
		t.Fatalf("policy bind type = %T, want string", args[0])
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(data), &rows); err != nil || len(rows) != 1 || rows[0]["knowledge_base_id"] != "a'" || rows[0]["lexical_weight"] != .5 || rows[0]["vector_weight"] != .5 {
		t.Fatalf("policy JSON = %+v, %v", rows, err)
	}
}
