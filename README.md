_This project has been created as part of the 42 curriculum by jpelline, anpollan, mhrvasm, zfarah and nraatika._

# Transcendence

## Description

We've creating a web-accessible multiplayer game.

### Team Information

#### Assigned roles

- **Product Owner**:
- **Project Manager**:
- **Technical Lead**:
- **Developers**:
- Brief description of their responsibilities.

#### Project Management

◦ How the team organized the work (task distribution, meetings, etc.).
◦ Tools used for project management (GitHub Issues, Trello, etc.).
◦ Communication channels used (Discord, Slack, etc.).

#### Technical Stack

◦ Frontend technologies and frameworks used.
◦ Backend technologies and frameworks used.
◦ Database system and why it was chosen.
◦ Any other significant technologies or libraries.
◦ Justification for major technical choices.

#### Database Schema

◦ Visual representation or description of the database structure.
◦ Tables/collections and their relationships.
◦ Key fields and data types.

#### Features List

◦ Complete list of implemented features.
◦ Which team member(s) worked on each feature.
◦ Brief description of each feature’s functionality

#### Modules

◦ List of all chosen modules (Major and Minor).
◦ Point calculation (Major = 2pts, Minor = 1pt).
◦ Justification for each module choice, especially for custom "Modules of
choice".
◦ How each module was implemented.
◦ Which team member(s) worked on each module

#### Individual Contributions

◦ Detailed breakdown of what each team member contributed.
◦ Specific features, modules, or components implemented by each person.
◦ Any challenges faced and how they were overcome.

---

## Instructions

To test out, you must create a `secrets/` folder parallell to the root of the repo, and make a file called `postgres_user_pw.txt` containing the password used to connect to the DB. You can then launch the backend and database containers with the command

```
docker compose up
```

which should result in:

- both containers launching
- the backend establishing a connection to the DB
- the backend creating the `users` table in the DB, as specified in `backend/db/migrations/`
- the backend starting to serve a dummy endpoint at `/api/health`

You can test the dummy endpoint by going to `localhost:8080/api/health` in a browser, or running

```
curl -i http://localhost:8080/api/health
```

It should return a status 200 OK response with the body

```
{"status": "ok", "message": "Go backend is alive!"}
```

---

## Resources

list of used resources
