// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared in its ports files — never on an adapter.
//
// The ports are the contract between the layers, and their doc comments are
// load-bearing: read a port's comment before writing a service that calls it
// or a repository that implements it. They live in one file per product
// slice, so a change to one area's contract is a small diff in one file:
//
//   - ports_platform.go: the platform ports: time, hashing, tokens and
//     outbound mail -- the infrastructure every slice leans on
//   - ports_identity.go: the identity slice's ports: users, sessions,
//     magic links, personal API tokens and the sign-in lockout ledger
//   - ports_identity_household.go: the identity slice's household ports:
//     the household row, memberships, spaces and notification preferences
//   - ports_identity_invite.go: the identity slice's invite ports:
//     inviting a member, the Telegram knock, and the pairing code
//   - ports_identity_signup.go: the identity slice's self-serve sign-up
//     ports
//   - ports_channel.go: the channel slice's ports: Telegram sending,
//     account linking, the invite hand-offs between the bot and
//     InviteService, and nudges
//   - ports_admin.go: the platform-admin slice's ports: admins, feature
//     flags, the audit log, re-authentication, the household directory,
//     the database browse and the outbound mail inspector
//   - ports_money.go: the money slice's core ports: exchange rates,
//     accounts, transactions, categories and budgets
//   - ports_money_goal.go: the money slice's goal ports
//   - ports_money_bill.go: the money slice's bill ports
//   - ports_money_holding.go: the money slice's investment-holding ports
//   - ports_marriage_retro.go: the marriage slice's monthly retro ports
//   - ports_marriage_vision.go: the marriage slice's vision ports,
//     including the one-method goal reader VisionService declares for
//     itself
//   - ports_marriage_agreement.go: the marriage slice's agreement ports
package usecase
