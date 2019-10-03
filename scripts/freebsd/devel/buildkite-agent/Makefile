# $FreeBSD$

PORTNAME=	buildkite-agent
PORTVERSION=	3.15.1
DISTVERSIONPREFIX= v
CATEGORIES=	devel

MAINTAINER=	support@buildkite.com
COMMENT=	Buildkite CI/CD SaaS Agent

LICENSE=	MIT

USES=		go
USE_GITHUB=	yes
GH_ACCOUNT=	buildkite
GH_PROJECT=	agent

RUNDEPENDS=	bash:shells/bash
USERS=		${PORTNAME}
GROUPS=		${PORTNAME}
USE_RC_SUBR=	${PORTNAME}

SUB_FILES=	buildkite-agent pkg-message buildkite-agent.cfg.sample

post-install:
	${MKDIR} ${STAGEDIR}${ETCDIR}
	(cd ${WRKSRC}/packaging/linux/root/usr/share/buildkite-agent/ && \
		${COPYTREE_SHARE} hooks/ ${STAGEDIR}${ETCDIR})
	${INSTALL_DATA} ${WRKDIR}/buildkite-agent.cfg.sample ${STAGEDIR}${ETCDIR}

.include <bsd.port.mk>
