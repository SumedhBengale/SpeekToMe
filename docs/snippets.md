# Snippets

A snippet is saved text that dictation inserts when the user says its name.
Saying "my address" on its own inserts the saved address; saying "I'll email
you my address" keeps the words, because there they are part of a sentence.

## Deciding an occurrence

`server/snippets` finds each name in the raw transcript as whole words,
ignoring case and punctuation, never across a sentence end or line break. The
longest name wins ("my work address" over "my work"). A leading "insert",
"paste" or "snippet" belongs to the occurrence and is replaced with it.

Each occurrence is then decided:

- **The whole dictation** is the name: inserted without asking.
- **Otherwise Jev decides**, through `vocabularydecision.Client.Choose` with the
  options `keep_words` / `insert_snippet`. Jev receives the transcript and the
  snippet's name, never its saved text.
- **Without a Jev answer** (timeout, rate limit, no key) the occurrence is
  inserted only when it is a sentence of its own or follows a command word. A
  snippet wrongly pasted mid-sentence is worse than a name left as words.

During a `/v1/stream` dictation the resolver decides an occurrence live once six
words follow it or it has waited two seconds, like live vocabulary decisions.
The end of the dictation reuses those decisions when the words are unchanged
and asks once for the rest, in parallel with vocabulary resolution. The
fallback stream and `/generate` decide everything at the end.

## Text assembly

Inserted snippets become locked spans of the raw transcript
(`Audit.ProtectWith`): the grammar model sees a marker instead of the snippet,
and vocabulary decisions inside an inserted span give way to it. The saved text
therefore reaches the user exactly as saved, line breaks included. If the
grammar model loses or duplicates a marker, the output falls back to the
unpolished transcript with snippets and vocabulary applied.

Snippets apply to dictation only, not to meeting transcripts.

## Storage and API

Table `snippets` (migration 3): `public_id` (24 hex characters, the API id),
`user_id`, `name`, `normalized_name` (unique per account), `content`,
timestamps. The content is sealed under the account key (`snippet-content`,
AAD bound to account and snippet id). Names stay plain text, like vocabulary,
so uniqueness can be enforced. Account deletion removes them.

Limits: 50 snippets on Free and 1,000 on Pro (including the 48-hour grace
period), enforced by one conditional insert. After a downgrade, every snippet
is kept and editable, and the oldest snippets up to the limit stay active.

| Method | Path | Body | Result |
| --- | --- | --- | --- |
| `GET` | `/v1/snippets` | | `{snippets, limit, count}` |
| `POST` | `/v1/snippets` | `{name, content}` | `201` snippet |
| `PUT` | `/v1/snippets/{id}` | `{name, content}` | snippet |
| `DELETE` | `/v1/snippets/{id}` | | `{status}` |

Names have 1–6 words and at most 60 characters; content is 1–5,000
characters. Errors carry `code`: `invalid_snippet`, `snippet_name_taken`
(409), `snippet_limit` (403), `snippet_not_found` (404).

The desktop app (Snippets tab) and the website dashboard (Snippets tab) both
manage snippets through this API.

## Checks

```sh
(cd server && go test ./snippets ./db ./handlers ./vocabularydecision)
```

`snippets/jev_acceptance_test.go` asks the real decision model about typical
dictations. It needs `TYPESAFE_API_KEY` and `SPEEKTOME_JEV_ACCEPTANCE=1`. On
2026-10-27 it answered 7 of 8 cases as expected. The one miss, a name at the
end of a sentence without a pause, kept the words.
