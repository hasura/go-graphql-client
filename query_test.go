package graphql

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

type cachedDirective struct {
	ttl int
}

func (cd cachedDirective) Type() OptionType {
	return OptionTypeOperationDirective
}

func (cd cachedDirective) String() string {
	if cd.ttl <= 0 {
		return "@cached"
	}
	return fmt.Sprintf("@cached(ttl: %d)", cd.ttl)
}

func TestConstructQuery(t *testing.T) {
	tests := []struct {
		options     []Option
		inV         interface{}
		inVariables map[string]interface{}
		want        string
	}{
		{
			inV: struct {
				Viewer struct {
					Login      string
					CreatedAt  DateTime
					ID         ID
					DatabaseID int
				}
				RateLimit struct {
					Cost      int
					Limit     int
					Remaining int
					ResetAt   DateTime
				}
			}{},
			want: `{viewer{login,createdAt,id,databaseId},rateLimit{cost,limit,remaining,resetAt}}`,
		},
		{
			options: []Option{OperationName("GetRepository"), cachedDirective{}},
			inV: struct {
				Repository struct {
					DatabaseID int
					URL        URI

					Issue struct {
						Comments struct {
							Edges []struct {
								Node struct {
									Body   string
									Author struct {
										Login string
									}
									Editor struct {
										Login string
									}
								}
								Cursor string
							}
						} `graphql:"comments(first:1after:\"Y3Vyc29yOjE5NTE4NDI1Ng==\")"`
					} `graphql:"issue(number:1)"`
				} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
			}{},
			want: `query GetRepository @cached {repository(owner:"shurcooL-test"name:"test-repo"){databaseId,url,issue(number:1){comments(first:1after:"Y3Vyc29yOjE5NTE4NDI1Ng=="){edges{node{body,author{login},editor{login}},cursor}}}}}`,
		},
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI
					URL       URI
				}

				return struct {
					Repository struct {
						DatabaseID int
						URL        URI

						Issue struct {
							Comments struct {
								Edges []struct {
									Node struct {
										DatabaseID      int
										Author          actor
										PublishedAt     DateTime
										LastEditedAt    *DateTime
										Editor          *actor
										Body            string
										ViewerCanUpdate bool
									}
									Cursor string
								}
							} `graphql:"comments(first:1)"`
						} `graphql:"issue(number:1)"`
					} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
				}{}
			}(),
			want: `{repository(owner:"shurcooL-test"name:"test-repo"){databaseId,url,issue(number:1){comments(first:1){edges{node{databaseId,author{login,avatarUrl,url},publishedAt,lastEditedAt,editor{login,avatarUrl,url},body,viewerCanUpdate},cursor}}}}}`,
		},
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI `graphql:"avatarUrl(size:72)"`
					URL       URI
				}

				return struct {
					Repository struct {
						Issue struct {
							Author         actor
							PublishedAt    DateTime
							LastEditedAt   *DateTime
							Editor         *actor
							Body           string
							ReactionGroups []struct {
								Content ReactionContent
								Users   struct {
									TotalCount int
								}
								ViewerHasReacted bool
							}
							ViewerCanUpdate bool

							Comments struct {
								Nodes []struct {
									DatabaseID     int
									Author         actor
									PublishedAt    DateTime
									LastEditedAt   *DateTime
									Editor         *actor
									Body           string
									ReactionGroups []struct {
										Content ReactionContent
										Users   struct {
											TotalCount int
										}
										ViewerHasReacted bool
									}
									ViewerCanUpdate bool
								}
								PageInfo struct {
									EndCursor   string
									HasNextPage bool
								}
							} `graphql:"comments(first:1)"`
						} `graphql:"issue(number:1)"`
					} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
				}{}
			}(),
			want: `{repository(owner:"shurcooL-test"name:"test-repo"){issue(number:1){author{login,avatarUrl(size:72),url},publishedAt,lastEditedAt,editor{login,avatarUrl(size:72),url},body,reactionGroups{content,users{totalCount},viewerHasReacted},viewerCanUpdate,comments(first:1){nodes{databaseId,author{login,avatarUrl(size:72),url},publishedAt,lastEditedAt,editor{login,avatarUrl(size:72),url},body,reactionGroups{content,users{totalCount},viewerHasReacted},viewerCanUpdate},pageInfo{endCursor,hasNextPage}}}}}`,
		},
		{
			inV: struct {
				Repository struct {
					Issue struct {
						Body string
					} `graphql:"issue(number: 1)"`
				} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
			}{},
			want: `{repository(owner:"shurcooL-test"name:"test-repo"){issue(number: 1){body}}}`,
		},
		{
			inV: struct {
				Repository struct {
					Issue struct {
						Body string
					} `graphql:"issue(number: $issueNumber)"`
				} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName)"`
			}{},
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
			},
			want: `query ($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!){repository(owner: $repositoryOwner, name: $repositoryName){issue(number: $issueNumber){body}}}`,
		},
		{
			inV: struct {
				Repository struct {
					Issue struct {
						ReactionGroups []struct {
							Users struct {
								Nodes []struct {
									Login string
								}
							} `graphql:"users(first:10)"`
						}
					} `graphql:"issue(number: $issueNumber)"`
				} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName)"`
			}{},
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
			},
			want: `query ($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!){repository(owner: $repositoryOwner, name: $repositoryName){issue(number: $issueNumber){reactionGroups{users(first:10){nodes{login}}}}}}`,
		},
		// check test above works with repository inner map
		{
			inV: func() interface{} {
				type query struct {
					Repository [][2]interface{} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName)"`
				}
				type issue struct {
					ReactionGroups []struct {
						Users struct {
							Nodes []struct {
								Login string
							}
						} `graphql:"users(first:10)"`
					}
				}
				return query{Repository: [][2]interface{}{
					{"issue(number: $issueNumber)", issue{}},
				}}
			}(),
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
			},
			want: `query ($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!){repository(owner: $repositoryOwner, name: $repositoryName){issue(number: $issueNumber){reactionGroups{users(first:10){nodes{login}}}}}}`,
		},
		// check inner maps work inside slices
		{
			inV: func() interface{} {
				type query struct {
					Repository [][2]interface{} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName)"`
				}
				type issue struct {
					ReactionGroups []struct {
						Users [][2]interface{} `graphql:"users(first:10)"`
					}
				}
				type nodes []struct {
					Login string
				}
				return query{Repository: [][2]interface{}{
					{"issue(number: $issueNumber)", issue{
						ReactionGroups: []struct {
							Users [][2]interface{} `graphql:"users(first:10)"`
						}{
							{Users: [][2]interface{}{
								{"nodes", nodes{}},
							}},
						},
					}},
				}}
			}(),
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
			},
			want: `query ($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!){repository(owner: $repositoryOwner, name: $repositoryName){issue(number: $issueNumber){reactionGroups{users(first:10){nodes{login}}}}}}`,
		},
		// Embedded structs without graphql tag should be inlined in query.
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI
					URL       URI
				}
				type event struct { // Common fields for all events.
					Actor     actor
					CreatedAt DateTime
				}
				type IssueComment struct {
					Body string
				}
				return struct {
					event                                         // Should be inlined.
					IssueComment  `graphql:"... on IssueComment"` // Should not be, because of graphql tag.
					CurrentTitle  string
					PreviousTitle string
					Label         struct {
						Name  string
						Color string
					}
				}{}
			}(),
			want: `{actor{login,avatarUrl,url},createdAt,... on IssueComment{body},currentTitle,previousTitle,label{name,color}}`,
		},
		{
			inV: struct {
				Viewer struct {
					Login      string
					CreatedAt  time.Time
					ID         interface{}
					DatabaseID int
				}
			}{},
			want: `{viewer{login,createdAt,id,databaseId}}`,
		},
		{
			inV: struct {
				Viewer struct {
					ID         interface{}
					Login      string
					CreatedAt  time.Time
					DatabaseID int
				}
				Tags map[string]interface{} `scalar:"true"`
			}{},
			want: `{viewer{id,login,createdAt,databaseId},tags}`,
		},
		{
			inV: struct {
				Viewer struct {
					ID         interface{}
					Login      string
					CreatedAt  time.Time
					DatabaseID int
				} `scalar:"true"`
			}{},
			want: `{viewer}`,
		},
		{
			inV: struct {
				Viewer struct {
					ID         interface{} `graphql:"-"`
					Login      string
					CreatedAt  time.Time `graphql:"-"`
					DatabaseID int
				}
			}{},
			want: `{viewer{login,databaseId}}`,
		},
	}
	for _, tc := range tests {
		got, err := ConstructQuery(tc.inV, tc.inVariables, tc.options...)
		if err != nil {
			t.Error(err)
		} else if got != tc.want {
			t.Errorf("\ngot:  %q\nwant: %q\n", got, tc.want)
		}
	}
}

type CreateUser struct {
	Login string
}

type DeleteUser struct {
	Login string
}

func TestConstructMutation(t *testing.T) {
	tests := []struct {
		inV         interface{}
		inVariables map[string]interface{}
		want        string
	}{
		{
			inV: struct {
				AddReaction struct {
					Subject struct {
						ReactionGroups []struct {
							Users struct {
								TotalCount int
							}
						}
					}
				} `graphql:"addReaction(input:$input)"`
			}{},
			inVariables: map[string]interface{}{
				"input": AddReactionInput{
					SubjectID: "MDU6SXNzdWUyMzE1MjcyNzk=",
					Content:   ReactionContentThumbsUp,
				},
			},
			want: `mutation ($input:AddReactionInput!){addReaction(input:$input){subject{reactionGroups{users{totalCount}}}}}`,
		},
		{
			inV: [][2]interface{}{
				{"createUser(login:$login1)", &CreateUser{}},
				{"deleteUser(login:$login2)", &DeleteUser{}},
			},
			inVariables: map[string]interface{}{
				"login1": "grihabor",
				"login2": "diman",
			},
			want: "mutation ($login1:String!$login2:String!){createUser(login:$login1){login}deleteUser(login:$login2){login}}",
		},
	}
	for _, tc := range tests {
		got, err := ConstructMutation(tc.inV, tc.inVariables)
		if err != nil {
			t.Error(err)
		} else if got != tc.want {
			t.Errorf("\ngot:  %q\nwant: %q\n", got, tc.want)
		}
	}
}

func TestConstructSubscription(t *testing.T) {
	tests := []struct {
		name        string
		inV         interface{}
		inVariables map[string]interface{}
		want        string
	}{
		{
			inV: struct {
				Viewer struct {
					Login      string
					CreatedAt  DateTime
					ID         ID
					DatabaseID int
				}
				RateLimit struct {
					Cost      int
					Limit     int
					Remaining int
					ResetAt   DateTime
				}
			}{},
			want: `subscription{viewer{login,createdAt,id,databaseId},rateLimit{cost,limit,remaining,resetAt}}`,
		},
		{
			name: "GetRepository",
			inV: struct {
				Repository struct {
					DatabaseID int
					URL        URI

					Issue struct {
						Comments struct {
							Edges []struct {
								Node struct {
									Body   string
									Author struct {
										Login string
									}
									Editor struct {
										Login string
									}
								}
								Cursor string
							}
						} `graphql:"comments(first:1after:\"Y3Vyc29yOjE5NTE4NDI1Ng==\")"`
					} `graphql:"issue(number:1)"`
				} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
			}{},
			want: `subscription GetRepository{repository(owner:"shurcooL-test"name:"test-repo"){databaseId,url,issue(number:1){comments(first:1after:"Y3Vyc29yOjE5NTE4NDI1Ng=="){edges{node{body,author{login},editor{login}},cursor}}}}}`,
		},
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI
					URL       URI
				}

				return struct {
					Repository struct {
						DatabaseID int
						URL        URI

						Issue struct {
							Comments struct {
								Edges []struct {
									Node struct {
										DatabaseID      int
										Author          actor
										PublishedAt     DateTime
										LastEditedAt    *DateTime
										Editor          *actor
										Body            string
										ViewerCanUpdate bool
									}
									Cursor string
								}
							} `graphql:"comments(first:1)"`
						} `graphql:"issue(number:1)"`
					} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
				}{}
			}(),
			want: `subscription{repository(owner:"shurcooL-test"name:"test-repo"){databaseId,url,issue(number:1){comments(first:1){edges{node{databaseId,author{login,avatarUrl,url},publishedAt,lastEditedAt,editor{login,avatarUrl,url},body,viewerCanUpdate},cursor}}}}}`,
		},
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI `graphql:"avatarUrl(size:72)"`
					URL       URI
				}

				return struct {
					Repository struct {
						Issue struct {
							Author         actor
							PublishedAt    DateTime
							LastEditedAt   *DateTime
							Editor         *actor
							Body           string
							ReactionGroups []struct {
								Content ReactionContent
								Users   struct {
									TotalCount int
								}
								ViewerHasReacted bool
							}
							ViewerCanUpdate bool

							Comments struct {
								Nodes []struct {
									DatabaseID     int
									Author         actor
									PublishedAt    DateTime
									LastEditedAt   *DateTime
									Editor         *actor
									Body           string
									ReactionGroups []struct {
										Content ReactionContent
										Users   struct {
											TotalCount int
										}
										ViewerHasReacted bool
									}
									ViewerCanUpdate bool
								}
								PageInfo struct {
									EndCursor   string
									HasNextPage bool
								}
							} `graphql:"comments(first:1)"`
						} `graphql:"issue(number:1)"`
					} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
				}{}
			}(),
			want: `subscription{repository(owner:"shurcooL-test"name:"test-repo"){issue(number:1){author{login,avatarUrl(size:72),url},publishedAt,lastEditedAt,editor{login,avatarUrl(size:72),url},body,reactionGroups{content,users{totalCount},viewerHasReacted},viewerCanUpdate,comments(first:1){nodes{databaseId,author{login,avatarUrl(size:72),url},publishedAt,lastEditedAt,editor{login,avatarUrl(size:72),url},body,reactionGroups{content,users{totalCount},viewerHasReacted},viewerCanUpdate},pageInfo{endCursor,hasNextPage}}}}}`,
		},
		{
			inV: struct {
				Repository struct {
					Issue struct {
						Body string
					} `graphql:"issue(number: 1)"`
				} `graphql:"repository(owner:\"shurcooL-test\"name:\"test-repo\")"`
			}{},
			want: `subscription{repository(owner:"shurcooL-test"name:"test-repo"){issue(number: 1){body}}}`,
		},
		{
			inV: struct {
				Repository struct {
					Issue struct {
						Body string
					} `graphql:"issue(number: $issueNumber)"`
				} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName)"`
			}{},
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
			},
			want: `subscription ($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!){repository(owner: $repositoryOwner, name: $repositoryName){issue(number: $issueNumber){body}}}`,
		},
		{
			name: "SearchRepository",
			inV: struct {
				Repository struct {
					Issue struct {
						ReactionGroups []struct {
							Users struct {
								Nodes []struct {
									Login string
								}
							} `graphql:"users(first:10)"`
						}
					} `graphql:"issue(number: $issueNumber)"`
				} `graphql:"repository(owner: $repositoryOwner, name: $repositoryName, review: $userReview)"`
			}{},
			inVariables: map[string]interface{}{
				"repositoryOwner": "shurcooL-test",
				"repositoryName":  "test-repo",
				"issueNumber":     1,
				"review":          UserReview{},
			},
			want: `subscription SearchRepository($issueNumber:Int!$repositoryName:String!$repositoryOwner:String!$review:user_review!){repository(owner: $repositoryOwner, name: $repositoryName, review: $userReview){issue(number: $issueNumber){reactionGroups{users(first:10){nodes{login}}}}}}`,
		},
		// Embedded structs without graphql tag should be inlined in query.
		{
			inV: func() interface{} {
				type actor struct {
					Login     string
					AvatarURL URI
					URL       URI
				}
				type event struct { // Common fields for all events.
					Actor     actor
					CreatedAt DateTime
				}
				type IssueComment struct {
					Body string
				}
				return struct {
					event                                         // Should be inlined.
					IssueComment  `graphql:"... on IssueComment"` // Should not be, because of graphql tag.
					CurrentTitle  string
					PreviousTitle string
					Label         struct {
						Name  string
						Color string
					}
				}{}
			}(),
			want: `subscription{actor{login,avatarUrl,url},createdAt,... on IssueComment{body},currentTitle,previousTitle,label{name,color}}`,
		},
		{
			inV: struct {
				Viewer struct {
					Login      string
					CreatedAt  time.Time
					ID         interface{}
					DatabaseID int
				}
			}{},
			want: `subscription{viewer{login,createdAt,id,databaseId}}`,
		},
	}
	for _, tc := range tests {
		got, err := ConstructSubscription(tc.inV, tc.inVariables, OperationName(tc.name))
		if err != nil {
			t.Error(err)
		} else if got != tc.want {
			t.Errorf("\ngot:  %q\nwant: %q\n", got, tc.want)
		}
	}
}

func TestQueryArguments(t *testing.T) {
	iVal := int(123)
	i8Val := int8(12)
	i16Val := int16(500)
	i32Val := int32(70000)
	i64Val := int64(5000000000)
	uiVal := uint(123)
	ui8Val := uint8(12)
	ui16Val := uint16(500)
	ui32Val := uint32(70000)
	ui64Val := uint64(5000000000)
	f32Val := float32(33.4)
	f64Val := float64(99.23)
	bVal := true
	sVal := "some string"
	tests := []struct {
		in   map[string]interface{}
		want string
	}{
		{
			in:   map[string]interface{}{"a": Int(123), "b": NewBoolean(true)},
			want: "$a:Int!$b:Boolean",
		},
		{
			in:   map[string]interface{}{"a": iVal, "b": i8Val, "c": i16Val, "d": i32Val, "e": i64Val, "f": Int(123)},
			want: "$a:Int!$b:Int!$c:Int!$d:Int!$e:Int!$f:Int!",
		},
		{
			in:   map[string]interface{}{"a": &iVal, "b": &i8Val, "c": &i16Val, "d": &i32Val, "e": &i64Val, "f": NewInt(123)},
			want: "$a:Int$b:Int$c:Int$d:Int$e:Int$f:Int",
		},
		{
			in:   map[string]interface{}{"a": uiVal, "b": ui8Val, "c": ui16Val, "d": ui32Val, "e": ui64Val},
			want: "$a:Int!$b:Int!$c:Int!$d:Int!$e:Int!",
		},
		{
			in:   map[string]interface{}{"a": &uiVal, "b": &ui8Val, "c": &ui16Val, "d": &ui32Val, "e": &ui64Val},
			want: "$a:Int$b:Int$c:Int$d:Int$e:Int",
		},
		{
			in:   map[string]interface{}{"a": f32Val, "b": f64Val, "c": Float(1.2)},
			want: "$a:Float!$b:Float!$c:Float!",
		},
		{
			in:   map[string]interface{}{"a": &f32Val, "b": &f64Val, "c": NewFloat(1.2)},
			want: "$a:Float$b:Float$c:Float",
		},
		{
			in:   map[string]interface{}{"a": &bVal, "b": bVal, "c": true, "d": false, "e": Boolean(true), "f": NewBoolean(true)},
			want: "$a:Boolean$b:Boolean!$c:Boolean!$d:Boolean!$e:Boolean!$f:Boolean",
		},
		{
			in:   map[string]interface{}{"a": NewID(123), "b": ID("id")},
			want: "$a:ID$b:ID!",
		},
		{
			in:   map[string]interface{}{"a": sVal, "b": &sVal, "c": String("foo"), "d": NewString("bar")},
			want: "$a:String!$b:String$c:String!$d:String",
		},
		{
			in: map[string]interface{}{
				"required": []IssueState{IssueStateOpen, IssueStateClosed},
				"optional": &[]IssueState{IssueStateOpen, IssueStateClosed},
			},
			want: "$optional:[IssueState!]$required:[IssueState!]!",
		},
		{
			in: map[string]interface{}{
				"required": []IssueState(nil),
				"optional": (*[]IssueState)(nil),
			},
			want: "$optional:[IssueState!]$required:[IssueState!]!",
		},
		{
			in: map[string]interface{}{
				"required": [...]IssueState{IssueStateOpen, IssueStateClosed},
				"optional": &[...]IssueState{IssueStateOpen, IssueStateClosed},
			},
			want: "$optional:[IssueState!]$required:[IssueState!]!",
		},
		{
			in:   map[string]interface{}{"id": NewID("someID")},
			want: "$id:ID",
		},
		{
			in:   map[string]interface{}{"id": ID("someID")},
			want: "$id:ID!",
		},
		{
			in:   map[string]interface{}{"ids": []ID{"someID", "anotherID"}},
			want: `$ids:[ID!]!`,
		},
		{
			in:   map[string]interface{}{"ids": &[]ID{"someID", "anotherID"}},
			want: `$ids:[ID!]`,
		},
		{
			in: map[string]interface{}{
				"id":           Uuid(uuid.New()),
				"id_optional":  &val,
				"ids":          []Uuid{},
				"ids_optional": []*Uuid{},
				"my_uuid":      MyUuid(uuid.New()),
				"review":       UserReview{},
				"review_input": UserReviewInput{},
			},
			want: `$id:uuid!$id_optional:uuid$ids:[uuid!]!$ids_optional:[uuid]!$my_uuid:my_uuid!$review:user_review!$review_input:user_review_input!`,
		},
	}
	for i, tc := range tests {
		got := queryArguments(tc.in)
		if got != tc.want {
			t.Errorf("test case %d:\n got: %q\nwant: %q", i, got, tc.want)
		}
	}
}

var val Uuid

type Uuid uuid.UUID

func (u Uuid) GetGraphQLType() string { return "uuid" }

type MyUuid Uuid

type UserReview struct {
	Review string
	UserID string
}

type UserReviewInput UserReview

func (u UserReview) GetGraphQLType() string { return "user_review" }

func (u UserReviewInput) GetGraphQLType() string { return "user_review_input" }

func (u MyUuid) GetGraphQLType() string { return "my_uuid" }

// Custom GraphQL types for testing.
type (
	// DateTime is an ISO-8601 encoded UTC date.
	DateTime struct{ time.Time }

	// URI is an RFC 3986, RFC 3987, and RFC 6570 (level 4) compliant URI.
	URI struct{ *url.URL }
)

func (u *URI) UnmarshalJSON(data []byte) error { panic("mock implementation") }

// IssueState represents the possible states of an issue.
type IssueState string

// The possible states of an issue.
const (
	IssueStateOpen   IssueState = "OPEN"   // An issue that is still open.
	IssueStateClosed IssueState = "CLOSED" // An issue that has been closed.
)

// ReactionContent represents emojis that can be attached to Issues, Pull Requests and Comments.
type ReactionContent string

// Emojis that can be attached to Issues, Pull Requests and Comments.
const (
	ReactionContentThumbsUp   ReactionContent = "THUMBS_UP"   // Represents the 👍 emoji.
	ReactionContentThumbsDown ReactionContent = "THUMBS_DOWN" // Represents the 👎 emoji.
	ReactionContentLaugh      ReactionContent = "LAUGH"       // Represents the 😄 emoji.
	ReactionContentHooray     ReactionContent = "HOORAY"      // Represents the 🎉 emoji.
	ReactionContentConfused   ReactionContent = "CONFUSED"    // Represents the 😕 emoji.
	ReactionContentHeart      ReactionContent = "HEART"       // Represents the ❤️ emoji.
)

// AddReactionInput is an autogenerated input type of AddReaction.
type AddReactionInput struct {
	// The Node ID of the subject to modify. (Required.)
	SubjectID ID `json:"subjectId"`
	// The name of the emoji to react with. (Required.)
	Content ReactionContent `json:"content"`

	// A unique identifier for the client performing the mutation. (Optional.)
	ClientMutationID *string `json:"clientMutationId,omitempty"`
}
